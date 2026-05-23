// Package api integration tests. We spin up a real chi router against a real
// temp SQLite database, drive it via httptest, and assert behavior across the
// handlers — emphasis on the cross-user isolation surface.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/feed"
	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/opml"
	"github.com/brandonhon/ember/internal/store"
)

type harness struct {
	t          *testing.T
	srv        *httptest.Server
	store      *store.Store
	auth       *auth.Auth
	dep        Dependencies
	noopPoller *fakePoller
}

type fakePoller struct {
	calls int
	feeds []int64
}

func (f *fakePoller) RefreshFeed(_ context.Context, id int64) error {
	f.calls++
	f.feeds = append(f.feeds, id)
	return nil
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	st := store.NewTest(t)
	a, err := auth.New(st, "0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	a.Params = auth.Params{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16}
	op := opml.NewService(st)
	fp := &fakePoller{}
	dep := Dependencies{
		Store: st, Auth: a, Poller: fp, OPML: op, TestMode: true,
	}
	r := NewRouter(dep)
	srv := httptest.NewTLSServer(r)
	t.Cleanup(srv.Close)

	return &harness{t: t, srv: srv, store: st, auth: a, dep: dep, noopPoller: fp}
}

func (h *harness) seedUser(t *testing.T, username, password string, admin bool) models.User {
	t.Helper()
	hash, err := h.auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	u, err := h.store.CreateUser(context.Background(), models.User{
		Username: username, PasswordHash: hash, IsAdmin: admin,
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// login posts /api/auth/login and returns a fresh *http.Client with its own
// cookie jar. We MUST NOT mutate h.srv.Client() — httptest returns the same
// instance every call and that would leak cookies across users.
func (h *harness) login(t *testing.T, username, password string) *http.Client {
	t.Helper()
	jar, _ := newJar()
	cl := h.newClient(jar)
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	resp, err := cl.Post(h.srv.URL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("login %s: %d %s", username, resp.StatusCode, string(raw))
	}
	return cl
}

// newClient returns a fresh client that trusts the test server's TLS cert.
func (h *harness) newClient(jar http.CookieJar) *http.Client {
	src := h.srv.Client()
	return &http.Client{Transport: src.Transport, Jar: jar}
}

func newJar() (http.CookieJar, error) {
	return cookiejarNew()
}

// json helpers ---------------------------------------------------------------

func get(t *testing.T, c *http.Client, url string, dst any) int {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if dst != nil {
		_ = json.NewDecoder(resp.Body).Decode(dst)
	}
	return resp.StatusCode
}

func post(t *testing.T, c *http.Client, url string, body any, dst any) int {
	t.Helper()
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	echoCSRF(c, url, req)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if dst != nil {
		_ = json.NewDecoder(resp.Body).Decode(dst)
	}
	return resp.StatusCode
}

func del(t *testing.T, c *http.Client, url string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	echoCSRF(c, url, req)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// echoCSRF reads the ember_csrf cookie from the client's jar (if present) and
// echoes it back as the X-Ember-CSRF header. No-op when no cookie is set
// (e.g. before login).
func echoCSRF(c *http.Client, rawURL string, req *http.Request) {
	if c.Jar == nil {
		return
	}
	u, err := neturlParse(rawURL)
	if err != nil {
		return
	}
	for _, ck := range c.Jar.Cookies(u) {
		if ck.Name == CSRFCookieName {
			req.Header.Set(CSRFHeaderName, ck.Value)
			return
		}
	}
}

// ---------------------------------------------------------------------------

func TestAuth_Login_LogoutMe(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "hunter2", false)

	// Unauthenticated /api/me → 401.
	anonJar, _ := newJar()
	anon := h.newClient(anonJar)
	if code := get(t, anon, h.srv.URL+"/api/me", nil); code != http.StatusUnauthorized {
		t.Errorf("anon /me = %d", code)
	}

	// Login.
	cl := h.login(t, "alice", "hunter2")

	// /me works.
	var me map[string]any
	if code := get(t, cl, h.srv.URL+"/api/me", &me); code != http.StatusOK {
		t.Errorf("/me = %d", code)
	}

	// Bad creds → 401.
	jar, _ := newJar()
	bad := h.newClient(jar)
	body, _ := json.Marshal(map[string]string{"username": "alice", "password": "wrong"})
	resp, _ := bad.Post(h.srv.URL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("bad creds = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Logout.
	if code := post(t, cl, h.srv.URL+"/api/auth/logout", nil, nil); code != http.StatusOK {
		t.Errorf("logout = %d", code)
	}
}

func TestCategories_CRUD_CrossUser(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "p", false)
	h.seedUser(t, "bob", "p", false)
	cA := h.login(t, "alice", "p")
	cB := h.login(t, "bob", "p")

	var created struct {
		Data models.Category `json:"data"`
	}
	if code := post(t, cA, h.srv.URL+"/api/categories",
		map[string]string{"name": "Tech"}, &created); code != http.StatusCreated {
		t.Fatalf("create category = %d", code)
	}

	// Bob's list doesn't see Alice's category.
	var bList struct {
		Data []models.Category `json:"data"`
	}
	get(t, cB, h.srv.URL+"/api/categories", &bList)
	if len(bList.Data) != 0 {
		t.Errorf("bob sees alice's category: %+v", bList.Data)
	}

	// Bob cannot delete it.
	if code := del(t, cB, fmt.Sprintf("%s/api/categories/%d", h.srv.URL, created.Data.ID)); code != http.StatusNotFound {
		t.Errorf("cross-user delete = %d, want 404", code)
	}

	// Alice can delete it.
	if code := del(t, cA, fmt.Sprintf("%s/api/categories/%d", h.srv.URL, created.Data.ID)); code != http.StatusOK {
		t.Errorf("delete = %d", code)
	}
}

func TestFeeds_AddRefreshDelete(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "p", false)
	cA := h.login(t, "alice", "p")

	var resp struct {
		Data struct {
			Feed         models.Feed         `json:"feed"`
			Subscription models.Subscription `json:"subscription"`
		} `json:"data"`
	}
	if code := post(t, cA, h.srv.URL+"/api/feeds",
		map[string]string{"url": "https://x.test/feed"}, &resp); code != http.StatusCreated {
		t.Fatalf("add feed = %d", code)
	}

	// Refresh (via subscription id).
	if code := post(t, cA, fmt.Sprintf("%s/api/feeds/%d/refresh", h.srv.URL, resp.Data.Subscription.ID), nil, nil); code != http.StatusOK {
		t.Errorf("refresh = %d", code)
	}
	// Need to wait briefly for the fire-and-forget refresh goroutine in add.
	time.Sleep(50 * time.Millisecond)
	if h.noopPoller.calls < 1 {
		t.Errorf("poller refresh calls = %d", h.noopPoller.calls)
	}

	// List.
	var list struct {
		Data []models.FeedWithCounts `json:"data"`
	}
	get(t, cA, h.srv.URL+"/api/feeds", &list)
	if len(list.Data) != 1 {
		t.Errorf("feed list len = %d", len(list.Data))
	}

	// Delete (unsubscribe).
	if code := del(t, cA, fmt.Sprintf("%s/api/feeds/%d", h.srv.URL, resp.Data.Subscription.ID)); code != http.StatusOK {
		t.Errorf("delete = %d", code)
	}
	get(t, cA, h.srv.URL+"/api/feeds", &list)
	if len(list.Data) != 0 {
		t.Errorf("feed list after delete = %d", len(list.Data))
	}
}

func TestArticles_StateAndCrossUser(t *testing.T) {
	h := newHarness(t)
	alice := h.seedUser(t, "alice", "p", false)
	bob := h.seedUser(t, "bob", "p", false)
	cA := h.login(t, "alice", "p")
	cB := h.login(t, "bob", "p")

	// Both subscribe to same feed.
	f, _ := h.store.UpsertFeed(context.Background(), models.Feed{URL: "https://x.test/feed", Title: "X"})
	_, _ = h.store.Subscribe(context.Background(), models.Subscription{UserID: alice.ID, FeedID: f.ID})
	_, _ = h.store.Subscribe(context.Background(), models.Subscription{UserID: bob.ID, FeedID: f.ID})
	a, _, _ := h.store.UpsertArticle(context.Background(), models.Article{
		FeedID: f.ID, GUID: "g1", Title: "Hello", ContentText: "world", ContentHash: "h1",
		PublishedAt: time.Now().Unix(), SummaryModel: "noop",
	})

	// Alice stars it.
	if code := post(t, cA, h.srv.URL+"/api/articles/star",
		map[string]any{"id": a.ID, "value": true}, nil); code != http.StatusOK {
		t.Errorf("star = %d", code)
	}

	// Bob's view shows is_starred=false.
	var bArt struct {
		Data models.ArticleView `json:"data"`
	}
	get(t, cB, fmt.Sprintf("%s/api/articles/%d", h.srv.URL, a.ID), &bArt)
	if bArt.Data.IsStarred {
		t.Error("Bob sees Alice's star")
	}

	// Alice's view shows is_starred=true.
	var aArt struct {
		Data models.ArticleView `json:"data"`
	}
	get(t, cA, fmt.Sprintf("%s/api/articles/%d", h.srv.URL, a.ID), &aArt)
	if !aArt.Data.IsStarred {
		t.Error("Alice's star didn't persist")
	}

	// Alice marks read.
	if code := post(t, cA, h.srv.URL+"/api/articles/read",
		map[string]any{"ids": []int64{a.ID}, "read": true}, nil); code != http.StatusOK {
		t.Errorf("read = %d", code)
	}

	// Mark-all-read for Bob should leave Alice alone (already read).
	if code := post(t, cB, h.srv.URL+"/api/articles/mark-all-read",
		map[string]int64{}, nil); code != http.StatusOK {
		t.Errorf("mark-all-read = %d", code)
	}
}

func TestArticles_FreshView(t *testing.T) {
	h := newHarness(t)
	u := h.seedUser(t, "alice", "p", false)
	c := h.login(t, "alice", "p")

	f, _ := h.store.UpsertFeed(context.Background(), models.Feed{URL: "https://x.test/feed", Title: "X"})
	_, _ = h.store.Subscribe(context.Background(), models.Subscription{UserID: u.ID, FeedID: f.ID})
	now := time.Now()
	_, _, _ = h.store.UpsertArticle(context.Background(), models.Article{
		FeedID: f.ID, GUID: "old", Title: "Old", ContentHash: "h1",
		PublishedAt: now.Add(-48 * time.Hour).Unix(), SummaryModel: "noop",
	})
	_, _, _ = h.store.UpsertArticle(context.Background(), models.Article{
		FeedID: f.ID, GUID: "new", Title: "New", ContentHash: "h2",
		PublishedAt: now.Add(-1 * time.Hour).Unix(), SummaryModel: "noop",
	})

	var resp struct {
		Data []models.ArticleView `json:"data"`
	}
	get(t, c, h.srv.URL+"/api/articles?view=fresh", &resp)
	if len(resp.Data) != 1 || resp.Data[0].GUID != "new" {
		t.Errorf("fresh view: %+v", resp.Data)
	}
}

func TestShares_FlowAndIsolation(t *testing.T) {
	h := newHarness(t)
	alice := h.seedUser(t, "alice", "p", false)
	bob := h.seedUser(t, "bob", "p", false)
	_ = h.seedUser(t, "carol", "p", false)
	cA := h.login(t, "alice", "p")
	cB := h.login(t, "bob", "p")
	cC := h.login(t, "carol", "p")

	f, _ := h.store.UpsertFeed(context.Background(), models.Feed{URL: "https://x.test/feed", Title: "X"})
	_, _ = h.store.Subscribe(context.Background(), models.Subscription{UserID: alice.ID, FeedID: f.ID})
	a, _, _ := h.store.UpsertArticle(context.Background(), models.Article{
		FeedID: f.ID, GUID: "g1", Title: "T", ContentHash: "h1", PublishedAt: time.Now().Unix(), SummaryModel: "noop",
	})

	// Alice shares to Bob.
	post(t, cA, h.srv.URL+"/api/shares", map[string]any{
		"article_id": a.ID, "to_user": bob.ID, "note": "interesting",
	}, nil)

	// Bob sees it in inbox.
	var bInbox struct {
		Data []models.Share `json:"data"`
	}
	get(t, cB, h.srv.URL+"/api/shares/inbox", &bInbox)
	if len(bInbox.Data) != 1 {
		t.Errorf("bob inbox = %d", len(bInbox.Data))
	}

	// Carol's inbox is empty (cross-user isolation).
	var cInbox struct {
		Data []models.Share `json:"data"`
	}
	get(t, cC, h.srv.URL+"/api/shares/inbox", &cInbox)
	if len(cInbox.Data) != 0 {
		t.Errorf("carol's inbox should be empty: %+v", cInbox.Data)
	}

	// Cannot share to self.
	if code := post(t, cA, h.srv.URL+"/api/shares", map[string]any{
		"article_id": a.ID, "to_user": alice.ID,
	}, nil); code != http.StatusBadRequest {
		t.Errorf("self-share = %d", code)
	}
}

func TestSearch_ScopedToUser(t *testing.T) {
	h := newHarness(t)
	alice := h.seedUser(t, "alice", "p", false)
	bob := h.seedUser(t, "bob", "p", false)
	cA := h.login(t, "alice", "p")

	// Alice subscribes to feed A; Bob to feed B.
	fa, _ := h.store.UpsertFeed(context.Background(), models.Feed{URL: "https://a.test/feed", Title: "A"})
	fb, _ := h.store.UpsertFeed(context.Background(), models.Feed{URL: "https://b.test/feed", Title: "B"})
	_, _ = h.store.Subscribe(context.Background(), models.Subscription{UserID: alice.ID, FeedID: fa.ID})
	_, _ = h.store.Subscribe(context.Background(), models.Subscription{UserID: bob.ID, FeedID: fb.ID})
	_, _, _ = h.store.UpsertArticle(context.Background(), models.Article{
		FeedID: fa.ID, GUID: "ga", Title: "Rust update", ContentText: "alice", ContentHash: "h1", PublishedAt: 1,
	})
	_, _, _ = h.store.UpsertArticle(context.Background(), models.Article{
		FeedID: fb.ID, GUID: "gb", Title: "Rust news", ContentText: "bob", ContentHash: "h2", PublishedAt: 2,
	})

	var res struct {
		Data []store.SearchResult `json:"data"`
	}
	get(t, cA, h.srv.URL+"/api/search?q=rust", &res)
	if len(res.Data) != 1 || res.Data[0].GUID != "ga" {
		t.Errorf("alice search should only see ga: %+v", res.Data)
	}

	// Empty query → 400.
	if code := get(t, cA, h.srv.URL+"/api/search", nil); code != http.StatusBadRequest {
		t.Errorf("empty q = %d", code)
	}
}

func TestAdmin_GateOnUserManagement(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "p", false)
	h.seedUser(t, "root", "p", true)
	cA := h.login(t, "alice", "p")
	cR := h.login(t, "root", "p")

	body := map[string]any{"username": "new", "password": "x"}

	// Non-admin → 403.
	if code := post(t, cA, h.srv.URL+"/api/users", body, nil); code != http.StatusForbidden {
		t.Errorf("non-admin create user = %d", code)
	}
	// Admin → 201.
	if code := post(t, cR, h.srv.URL+"/api/users", body, nil); code != http.StatusCreated {
		t.Errorf("admin create user = %d", code)
	}
	// Anon → 401.
	jar, _ := newJar()
	bad := h.newClient(jar)
	resp, _ := bad.Post(h.srv.URL+"/api/users", "application/json",
		bytes.NewReader(mustJSON(body)))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anon create user = %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestOPMLRoundtrip(t *testing.T) {
	h := newHarness(t)
	u := h.seedUser(t, "alice", "p", false)
	c := h.login(t, "alice", "p")

	// Import.
	const opmlBody = `<?xml version="1.0"?>
<opml version="2.0"><head><title>x</title></head><body>
  <outline title="Tech" text="Tech">
    <outline type="rss" title="X Blog" xmlUrl="https://x.test/feed" htmlUrl="https://x.test"/>
  </outline>
  <outline type="rss" title="Y Blog" xmlUrl="https://y.test/feed"/>
</body></opml>`
	body, ct := makeMultipart("file", "subs.opml", []byte(opmlBody))
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/api/feeds/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	echoCSRF(c, h.srv.URL+"/api/feeds/import", req)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("import = %d: %s", resp.StatusCode, string(raw))
	}
	resp.Body.Close()

	// User should now have 2 subscriptions and 1 category.
	cats, _ := h.store.ListCategories(context.Background(), u.ID)
	if len(cats) != 1 || cats[0].Name != "Tech" {
		t.Errorf("imported categories: %+v", cats)
	}
	feeds, _ := h.store.ListFeedsForUser(context.Background(), u.ID)
	if len(feeds) != 2 {
		t.Errorf("imported feeds = %d, want 2", len(feeds))
	}

	// Export.
	resp, err = c.Get(h.srv.URL + "/api/feeds/export")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export = %d", resp.StatusCode)
	}
	out, _ := io.ReadAll(resp.Body)
	body2 := string(out)
	if !strings.Contains(body2, `xmlUrl="https://x.test/feed"`) {
		t.Errorf("exported OPML missing x.test feed: %s", body2)
	}
}

func TestServer_APINotFound(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "p", false)
	c := h.login(t, "alice", "p")
	if code := get(t, c, h.srv.URL+"/api/no-such-endpoint", nil); code != http.StatusNotFound {
		t.Errorf("unknown /api = %d", code)
	}
}

func TestFever_Auth(t *testing.T) {
	h := newHarness(t)
	u := h.seedUser(t, "alice", "p", false)

	jar, _ := newJar()
	cl := h.newClient(jar)
	// Bad key → auth:0.
	resp, err := cl.Post(h.srv.URL+"/fever", "application/x-www-form-urlencoded",
		strings.NewReader("api_key=garbage"))
	if err != nil {
		t.Fatal(err)
	}
	var bad map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&bad)
	resp.Body.Close()
	if v, _ := bad["auth"].(float64); v != 0 {
		t.Errorf("bad-key auth = %v", v)
	}

	// Good key → auth:1, can fetch feeds.
	key := FeverKey(u.Username, fmt.Sprintf("%d", u.ID))
	resp2, err := cl.Post(h.srv.URL+"/fever?feeds", "application/x-www-form-urlencoded",
		strings.NewReader("api_key="+key))
	if err != nil {
		t.Fatal(err)
	}
	var good map[string]any
	_ = json.NewDecoder(resp2.Body).Decode(&good)
	resp2.Body.Close()
	if v, _ := good["auth"].(float64); v != 1 {
		t.Errorf("good-key auth = %v", v)
	}
}

func TestFilters_CRUDAndIsolation(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "p", false)
	h.seedUser(t, "bob", "p", false)
	cA := h.login(t, "alice", "p")
	cB := h.login(t, "bob", "p")

	// Alice creates a filter.
	var created struct {
		Data models.Filter `json:"data"`
	}
	body := map[string]any{
		"name":       "hide crypto",
		"match_json": `{"field":"title","op":"contains","value":"crypto"}`,
		"action":     "hide",
	}
	if code := post(t, cA, h.srv.URL+"/api/filters", body, &created); code != http.StatusCreated {
		t.Fatalf("create filter = %d", code)
	}

	// Validation: invalid match shape → 400.
	bad := map[string]any{
		"name": "x", "match_json": `{"field":"bogus","op":"contains","value":"y"}`, "action": "mark_read",
	}
	if code := post(t, cA, h.srv.URL+"/api/filters", bad, nil); code != http.StatusBadRequest {
		t.Errorf("invalid filter = %d, want 400", code)
	}
	// Validation: invalid action.
	bad2 := map[string]any{
		"name": "y", "match_json": `{"field":"title","op":"contains","value":"z"}`, "action": "delete_everything",
	}
	if code := post(t, cA, h.srv.URL+"/api/filters", bad2, nil); code != http.StatusBadRequest {
		t.Errorf("invalid action = %d, want 400", code)
	}

	// Alice lists.
	var aList struct {
		Data []models.Filter `json:"data"`
	}
	get(t, cA, h.srv.URL+"/api/filters", &aList)
	if len(aList.Data) != 1 {
		t.Errorf("alice's filters = %d", len(aList.Data))
	}

	// Bob's list is empty (cross-user isolation).
	var bList struct {
		Data []models.Filter `json:"data"`
	}
	get(t, cB, h.srv.URL+"/api/filters", &bList)
	if len(bList.Data) != 0 {
		t.Errorf("bob sees alice's filters: %+v", bList.Data)
	}

	// Bob cannot patch or delete Alice's filter.
	if code := del(t, cB, fmt.Sprintf("%s/api/filters/%d", h.srv.URL, created.Data.ID)); code != http.StatusNotFound {
		t.Errorf("cross-user delete = %d", code)
	}

	// Alice patches enabled=false.
	disabled := false
	patchBody, _ := json.Marshal(map[string]any{"enabled": disabled})
	req, _ := http.NewRequest(http.MethodPatch,
		fmt.Sprintf("%s/api/filters/%d", h.srv.URL, created.Data.ID), bytes.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	echoCSRF(cA, h.srv.URL, req)
	resp, err := cA.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("patch filter = %d", resp.StatusCode)
	}

	// Alice deletes.
	if code := del(t, cA, fmt.Sprintf("%s/api/filters/%d", h.srv.URL, created.Data.ID)); code != http.StatusOK {
		t.Errorf("delete filter = %d", code)
	}
}

func TestStaticFallback(t *testing.T) {
	st := store.NewTest(t)
	a, _ := auth.New(st, "0123456789abcdef0123456789abcdef")
	a.Params = auth.Params{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16}
	op := opml.NewService(st)
	static := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>index</html>"))
	})
	r := NewRouter(Dependencies{Store: st, Auth: a, OPML: op, StaticH: static})
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/some/spa/route")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("spa fallback = %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "index") {
		t.Errorf("spa body = %q", string(body))
	}

	// /api/* still 404s instead of falling back.
	resp2, err := srv.Client().Get(srv.URL + "/api/no-such")
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("/api/no-such = %d", resp2.StatusCode)
	}
}

// makeMultipart returns a body and a multipart Content-Type containing the named
// field with `filename` and `data`.
func makeMultipart(field, filename string, data []byte) ([]byte, string) {
	var buf bytes.Buffer
	mw := mwNew(&buf)
	fw, _ := mw.CreateFormFile(field, filename)
	_, _ = fw.Write(data)
	_ = mw.Close()
	return buf.Bytes(), mw.FormDataContentType()
}

// mwNew indirection so we can import multipart in one place.
func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// Test feed package was wired correctly (imported elsewhere transitively;
// ensure the import path didn't drift).
var _ = feed.DefaultUserAgent
