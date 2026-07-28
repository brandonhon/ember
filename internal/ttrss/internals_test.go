package ttrss

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// A Service with no ValidateURL must refuse ALL redirects rather than follow
// them unchecked. ValidateURL is always set in production; this is the
// fail-safe for a misconfigured zero-value Service, and following a redirect
// chain there would be an unguarded SSRF path.
func TestAPIClient_BlocksRedirectsWithoutValidator(t *testing.T) {
	s := &Service{}
	c := s.apiClient(context.Background())
	if c.CheckRedirect == nil {
		t.Fatal("no CheckRedirect installed — redirects would be followed unchecked")
	}
	req, _ := http.NewRequest(http.MethodGet, "http://example.test/", nil)
	if err := c.CheckRedirect(req, nil); err == nil {
		t.Error("redirect allowed with no ValidateURL configured")
	}
}

// With a validator, the redirect guard delegates to it — a rejected hop must
// stop the chain.
func TestAPIClient_RedirectGuardConsultsValidator(t *testing.T) {
	var seen string
	s := &Service{ValidateURL: func(_ context.Context, raw string) error {
		seen = raw
		return errors.New("blocked by policy")
	}}
	c := s.apiClient(context.Background())
	req, _ := http.NewRequest(http.MethodGet, "http://redirected.test/x", nil)
	if err := c.CheckRedirect(req, nil); err == nil {
		t.Error("redirect allowed despite the validator rejecting it")
	}
	if seen != "http://redirected.test/x" {
		t.Errorf("validator saw %q, want the redirect target", seen)
	}
}

// An injected client is used verbatim (that is how the existing API tests
// point the service at an httptest server).
func TestAPIClient_HonorsInjectedClient(t *testing.T) {
	own := &http.Client{}
	s := &Service{HTTPClient: own}
	if got := s.apiClient(context.Background()); got != own {
		t.Error("apiClient ignored the injected HTTPClient")
	}
}

// TT-RSS is inconsistent about id types across versions and endpoints:
// getCategories may return quoted strings while getFeeds returns numbers.
// Both ends of the folder mapping have to tolerate either, and the negative
// ids TT-RSS uses for virtual feeds (-1 starred, -2 published) must survive.
func TestFlexInt_AcceptsBothWireForms(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		want    flexInt
		wantErr bool
	}{
		{`123`, 123, false},
		{`"123"`, 123, false},
		{`-1`, -1, false},   // TT-RSS virtual feed: starred
		{`"-2"`, -2, false}, // published, quoted
		{`0`, 0, false},
		{`null`, 0, false},
		{`""`, 0, false},
		{`"  42  "`, 42, false},
		{`"abc"`, 0, true},
		{`"1.5"`, 0, true},
	} {
		var f flexInt
		err := json.Unmarshal([]byte(tc.raw), &f)
		if (err != nil) != tc.wantErr {
			t.Errorf("Unmarshal(%s) err = %v, wantErr %v", tc.raw, err, tc.wantErr)
			continue
		}
		if err == nil && f != tc.want {
			t.Errorf("Unmarshal(%s) = %d, want %d", tc.raw, f, tc.want)
		}
	}
}

// flexInt must work as a struct field, which is how it is actually used.
func TestFlexInt_InStructDecoding(t *testing.T) {
	var c ttCategory
	if err := json.Unmarshal([]byte(`{"id":"7","title":"Tech"}`), &c); err != nil {
		t.Fatal(err)
	}
	if c.ID != 7 || c.Title != "Tech" {
		t.Errorf("got %+v, want id=7 title=Tech", c)
	}
	var f ttFeed
	if err := json.Unmarshal([]byte(`{"id":3,"title":"F","feed_url":"u","cat_id":"9"}`), &f); err != nil {
		t.Fatal(err)
	}
	if f.ID != 3 || f.CatID != 9 {
		t.Errorf("got %+v, want id=3 cat_id=9", f)
	}
}

func newSvc(t *testing.T) (*Service, int64) {
	t.Helper()
	st := store.NewTest(t)
	u, err := st.CreateUser(context.Background(), models.User{Username: "alice", PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	return NewService(st), u.ID
}

// TT-RSS's "Uncategorized" pseudo-folder, and any non-positive or unnamed
// category, must map to "no ember category" — not to a folder literally
// called Uncategorized.
func TestResolveCategory_UncategorizedAndSentinelsMapToNil(t *testing.T) {
	svc, uid := newSvc(t)
	ctx := context.Background()
	names := map[int]string{
		0:  "Uncategorized",
		-1: "Special",
		5:  "  ",
		7:  "uncategorized", // case-insensitive
	}
	for _, id := range []int{0, -1, 5, 7} {
		got, err := svc.resolveCategory(ctx, uid, id, names, map[int]*int64{})
		if err != nil {
			t.Fatalf("resolveCategory(%d): %v", id, err)
		}
		if got != nil {
			t.Errorf("resolveCategory(%d) = %v, want nil", id, got)
		}
	}
	cats, _ := svc.Store.ListCategories(ctx, uid)
	if len(cats) != 0 {
		t.Errorf("created %d categories for sentinel ids: %+v", len(cats), cats)
	}
}

// A real category is created once and then served from the memo — importing
// 500 feeds in one folder must not issue 500 CreateCategory calls.
func TestResolveCategory_CreatesOnceAndMemoizes(t *testing.T) {
	svc, uid := newSvc(t)
	ctx := context.Background()
	names := map[int]string{3: "Technology"}
	memo := map[int]*int64{}

	first, err := svc.resolveCategory(ctx, uid, 3, names, memo)
	if err != nil || first == nil {
		t.Fatalf("first resolve = %v, %v", first, err)
	}
	second, err := svc.resolveCategory(ctx, uid, 3, names, memo)
	if err != nil {
		t.Fatal(err)
	}
	if second == nil || *second != *first {
		t.Errorf("memo returned %v, want the same id as %v", second, first)
	}
	cats, _ := svc.Store.ListCategories(ctx, uid)
	if len(cats) != 1 {
		t.Errorf("created %d categories, want 1", len(cats))
	}
}

// A folder whose name already exists in ember is reused, not duplicated —
// re-running a migrate must not pile up "Technology" folders.
func TestResolveCategory_ReusesExistingByName(t *testing.T) {
	svc, uid := newSvc(t)
	ctx := context.Background()
	existing, err := svc.Store.CreateCategory(ctx, models.Category{UserID: uid, Name: "Technology"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.resolveCategory(ctx, uid, 3, map[int]string{3: "Technology"}, map[int]*int64{})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != existing.ID {
		t.Errorf("resolveCategory = %v, want the existing id %d", got, existing.ID)
	}
	cats, _ := svc.Store.ListCategories(ctx, uid)
	if len(cats) != 1 {
		t.Errorf("have %d categories, want 1 (reused)", len(cats))
	}
}

// The parked import feed is created once, is non-muted (so its articles show
// in the Starred smart view, which excludes muted feeds), and is parked far in
// the future so the poller never tries to fetch ttrss-import://.
func TestEnsureImportFeed_IdempotentParkedAndUnmuted(t *testing.T) {
	svc, uid := newSvc(t)
	ctx := context.Background()

	id1, err := svc.ensureImportFeed(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := svc.ensureImportFeed(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Errorf("ensureImportFeed returned %d then %d, want the same feed", id1, id2)
	}

	feeds, err := svc.Store.ListFeedsForUser(ctx, uid, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 1 {
		t.Fatalf("subscribed to %d feeds, want 1", len(feeds))
	}
	f := feeds[0]
	if f.Muted {
		t.Error("import feed is muted — its articles would be hidden from the Starred view")
	}
	if f.NextFetch != parkedNextFetch {
		t.Errorf("next_fetch = %d, want %d (parked so the poller never fetches it)", f.NextFetch, parkedNextFetch)
	}
	if f.URL == "" || f.Title != importFeedTitle {
		t.Errorf("feed = %+v, want the import placeholder", f)
	}
}
