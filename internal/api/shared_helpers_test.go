package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// Add-feed and edit-feed both resolve their URL through resolveFeedURL, so the
// SSRF guard must reject a private target on either path — not just the one it
// was originally written for.
func TestFeeds_AddAndUpdateRejectPrivateURL(t *testing.T) {
	h := newHarnessWith(t, func(d *Dependencies) { d.AllowPrivateURLs = false })
	u := h.seedUser(t, "alice", "p", false)
	c := h.login(t, "alice", "p")

	// Literal IPs so the check never depends on DNS.
	if code := post(t, c, h.srv.URL+"/api/feeds",
		map[string]any{"url": "http://127.0.0.1/feed"}, nil); code != http.StatusBadRequest {
		t.Errorf("add private feed = %d, want 400", code)
	}

	f, _ := h.store.UpsertFeed(context.Background(), models.Feed{URL: "https://x.test/feed", Title: "X"})
	sub, _ := h.store.Subscribe(context.Background(), models.Subscription{UserID: u.ID, FeedID: f.ID})
	if code := patch(t, c, fmt.Sprintf("%s/api/feeds/%d", h.srv.URL, sub.ID),
		map[string]any{"url": "http://169.254.169.254/latest/meta-data"}, nil); code != http.StatusBadRequest {
		t.Errorf("repoint to private URL = %d, want 400", code)
	}
}

// viewFreshAfter backs both the article list and mark-all-read, so a named view
// must bound what mark-all-read touches the same way it bounds the list: an
// article outside the Fresh window stays unread.
func TestMarkAllRead_HonorsViewWindow(t *testing.T) {
	h := newHarness(t)
	u := h.seedUser(t, "alice", "p", false)
	c := h.login(t, "alice", "p")

	f, _ := h.store.UpsertFeed(context.Background(), models.Feed{URL: "https://x.test/feed", Title: "X"})
	_, _ = h.store.Subscribe(context.Background(), models.Subscription{UserID: u.ID, FeedID: f.ID})
	now := time.Now()
	for _, a := range []struct {
		guid string
		age  time.Duration
	}{{"old", 48 * time.Hour}, {"new", time.Hour}} {
		if _, _, err := h.store.UpsertArticle(context.Background(), models.Article{
			FeedID: f.ID, GUID: a.guid, Title: a.guid, ContentHash: a.guid,
			PublishedAt: now.Add(-a.age).Unix(), SummaryModel: "noop",
		}); err != nil {
			t.Fatal(err)
		}
	}

	var marked struct {
		Data struct {
			Count int64 `json:"count"`
		} `json:"data"`
	}
	if code := post(t, c, h.srv.URL+"/api/articles/mark-all-read",
		map[string]any{"view": "fresh"}, &marked); code != http.StatusOK {
		t.Fatalf("mark-all-read view=fresh = %d, want 200", code)
	}
	if marked.Data.Count != 1 {
		t.Errorf("marked %d articles, want 1 (only the one inside the Fresh window)", marked.Data.Count)
	}

	// The out-of-window article is still unread and still listed.
	var list struct {
		Data []models.ArticleView `json:"data"`
	}
	get(t, c, h.srv.URL+"/api/articles?unread=1", &list)
	if len(list.Data) != 1 || list.Data[0].GUID != "old" {
		t.Errorf("unread after mark-all-read view=fresh: %+v, want just \"old\"", list.Data)
	}
}

// Mark-all-read must clear exactly what the list it sits under shows. With the
// summary gate on, a still-summarizing article is hidden from view=unread, and
// a muted feed's articles are excluded from smart views; marking either read
// would mean the user never sees it unread.
func TestMarkAllRead_SkipsGatedAndMutedArticles(t *testing.T) {
	h := newHarnessWith(t, func(d *Dependencies) {
		wireOllama(d, "http://ollama.invalid", "llama3")
		d.SummaryGraceSecondsFallback = 3600
	})
	u := h.seedUser(t, "alice", "p", false)
	c := h.login(t, "alice", "p")
	ctx := context.Background()

	loud, _ := h.store.UpsertFeed(ctx, models.Feed{URL: "https://loud.test/feed", Title: "Loud"})
	quiet, _ := h.store.UpsertFeed(ctx, models.Feed{URL: "https://quiet.test/feed", Title: "Quiet"})
	_, _ = h.store.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: loud.ID})
	quietSub, _ := h.store.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: quiet.ID})
	muted := true
	if err := h.store.UpdateSubscription(ctx, u.ID, quietSub.ID, store.UpdateSubscriptionPatch{Muted: &muted}); err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	for _, a := range []struct {
		feed  int64
		guid  string
		model string
	}{
		{loud.ID, "visible", "llama3"},
		{loud.ID, "summarizing", ""}, // fetched just now: inside the grace window, so hidden
		{quiet.ID, "muted", "llama3"},
	} {
		if _, _, err := h.store.UpsertArticle(ctx, models.Article{
			FeedID: a.feed, GUID: a.guid, Title: a.guid, ContentHash: a.guid,
			PublishedAt: now, SummaryModel: a.model,
		}); err != nil {
			t.Fatal(err)
		}
	}

	var marked struct {
		Data struct {
			Count int64 `json:"count"`
		} `json:"data"`
	}
	if code := post(t, c, h.srv.URL+"/api/articles/mark-all-read",
		map[string]any{"view": "unread"}, &marked); code != http.StatusOK {
		t.Fatalf("mark-all-read view=unread = %d, want 200", code)
	}
	if marked.Data.Count != 1 {
		t.Errorf("marked %d articles, want 1 (only the visible one)", marked.Data.Count)
	}

	// ?all=1 bypasses the gate and a muted feed is still listed by feed_id, so
	// both hidden articles must show up here as still unread.
	var list struct {
		Data []models.ArticleView `json:"data"`
	}
	get(t, c, fmt.Sprintf("%s/api/articles?unread=1&all=1&feed_id=%d", h.srv.URL, loud.ID), &list)
	if len(list.Data) != 1 || list.Data[0].GUID != "summarizing" {
		t.Errorf("loud unread after mark-all-read: %+v, want just \"summarizing\"", list.Data)
	}
	get(t, c, fmt.Sprintf("%s/api/articles?unread=1&feed_id=%d", h.srv.URL, quiet.ID), &list)
	if len(list.Data) != 1 || list.Data[0].GUID != "muted" {
		t.Errorf("muted feed unread after mark-all-read: %+v, want just \"muted\"", list.Data)
	}
}

// Every admin LLM endpoint runs through requireSummarizer; with no Ollama wired
// (the default in tests) each must 503 rather than nil-panic. GET /admin/llm is
// the exception — it reports enabled=false so the UI can render the setup hint.
func TestLLM_EndpointsWithoutSummarizer(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "root", "hunter2", true)
	c := h.login(t, "root", "hunter2")

	var status struct {
		Data llmStatus `json:"data"`
	}
	if code := get(t, c, h.srv.URL+"/api/admin/llm", &status); code != http.StatusOK {
		t.Fatalf("GET /api/admin/llm = %d, want 200", code)
	}
	if status.Data.Enabled {
		t.Error("enabled=true with no summarizer configured")
	}

	for _, path := range []string{"/api/admin/llm/model", "/api/admin/llm/pull", "/api/admin/llm/delete"} {
		if code := post(t, c, h.srv.URL+path, map[string]any{"model": "llama3"}, nil); code != http.StatusServiceUnavailable {
			t.Errorf("POST %s = %d, want 503", path, code)
		}
	}
	if code := post(t, c, h.srv.URL+"/api/admin/llm/options",
		map[string]any{"temperature": 0.5}, nil); code != http.StatusServiceUnavailable {
		t.Errorf("POST /api/admin/llm/options = %d, want 503", code)
	}
}

// modelFromRequest rejects a missing or malformed model reference before it
// reaches the Ollama daemon. Exercised with a summarizer wired so the 503 from
// requireSummarizer can't mask the validation.
func TestLLM_ModelNameValidation(t *testing.T) {
	h := newHarnessWith(t, func(d *Dependencies) { wireOllama(d, "http://ollama.invalid", "llama3") })
	h.seedUser(t, "root", "hunter2", true)
	c := h.login(t, "root", "hunter2")

	for _, tc := range []struct {
		name  string
		model string
	}{
		{"empty", ""},
		{"path traversal", "../../etc/passwd"},
		{"leading dash", "-rf"},
		{"space", "llama3 evil"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code := post(t, c, h.srv.URL+"/api/admin/llm/model",
				map[string]any{"model": tc.model}, nil); code != http.StatusBadRequest {
				t.Errorf("POST model=%q = %d, want 400", tc.model, code)
			}
		})
	}
}

// The passkey begin/finish endpoints share one request and one response type
// across the register and login ceremonies. Without EMBER_PUBLIC_URL there's no
// WebAuthn config, so all four must 503 instead of nil-panicking.
func TestPasskeys_WithoutWebAuthn(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "p", false)
	c := h.login(t, "alice", "p")

	for _, path := range []string{
		"/api/me/passkeys/register/begin",
		"/api/me/passkeys/register/finish",
		"/api/auth/passkey/begin",
		"/api/auth/passkey/finish",
	} {
		if code := post(t, c, h.srv.URL+path,
			map[string]any{"username": "alice", "session_id": "x"}, nil); code != http.StatusServiceUnavailable {
			t.Errorf("POST %s = %d, want 503", path, code)
		}
	}
}
