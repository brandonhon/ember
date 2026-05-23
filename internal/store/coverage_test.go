package store

import (
	"context"
	"errors"
	"testing"

	"github.com/brandonhon/ember/internal/models"
)

// TestCoverage_PatchBranches exercises the various pointer-field branches in
// the sparse update patches that the focused tests don't already cover.
func TestCoverage_PatchBranches(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})

	// UpdateUser: Email, SettingsJSON branches.
	email := "u@example.com"
	settings := `{"theme":"dark"}`
	if err := s.UpdateUser(ctx, u.ID, UpdateUserPatch{
		Email: &email, SettingsJSON: &settings,
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetUser(ctx, u.ID)
	if got.Email != email || got.SettingsJSON != settings {
		t.Errorf("update fields lost: %+v", got)
	}

	// UpdateCategory: Color, Position branches.
	c, _ := s.CreateCategory(ctx, models.Category{UserID: u.ID, Name: "C"})
	color := "#000"
	pos := 5
	if err := s.UpdateCategory(ctx, u.ID, c.ID, UpdateCategoryPatch{
		Color: &color, Position: &pos,
	}); err != nil {
		t.Fatal(err)
	}
	gotC, _ := s.GetCategory(ctx, u.ID, c.ID)
	if gotC.Color != color || gotC.Position != pos {
		t.Errorf("category update lost: %+v", gotC)
	}
	// Empty patch is a no-op.
	if err := s.UpdateCategory(ctx, u.ID, c.ID, UpdateCategoryPatch{}); err != nil {
		t.Errorf("empty patch should be ok: %v", err)
	}

	// UpdateFilter: Name, MatchJSON, Action branches.
	f, _ := s.CreateFilter(ctx, models.Filter{
		UserID: u.ID, Name: "x", MatchJSON: "{}", Action: "mark_read", Enabled: true,
	})
	name := "renamed"
	match := `{"field":"title","op":"contains","value":"foo"}`
	action := "star"
	if err := s.UpdateFilter(ctx, u.ID, f.ID, UpdateFilterPatch{
		Name: &name, MatchJSON: &match, Action: &action,
	}); err != nil {
		t.Fatal(err)
	}
	gotF, _ := s.GetFilter(ctx, u.ID, f.ID)
	if gotF.Name != name || gotF.Action != action || gotF.MatchJSON != match {
		t.Errorf("filter update lost: %+v", gotF)
	}
	// Empty patch is a no-op.
	if err := s.UpdateFilter(ctx, u.ID, f.ID, UpdateFilterPatch{}); err != nil {
		t.Errorf("empty filter patch should be ok: %v", err)
	}

	// UpdateSubscription: TitleOverride + CategoryID together; empty patch.
	feed, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})
	sub, _ := s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: feed.ID})
	title := "My Title"
	if err := s.UpdateSubscription(ctx, u.ID, sub.ID, UpdateSubscriptionPatch{
		TitleOverride: &title, CategoryID: &c.ID,
	}); err != nil {
		t.Fatal(err)
	}
	gotSub, _ := s.GetSubscriptionByID(ctx, u.ID, sub.ID)
	if gotSub.TitleOverride != title || gotSub.CategoryID == nil {
		t.Errorf("sub update lost: %+v", gotSub)
	}
	if err := s.UpdateSubscription(ctx, u.ID, sub.ID, UpdateSubscriptionPatch{}); err != nil {
		t.Errorf("empty sub patch should be ok: %v", err)
	}
}

func TestCoverage_NotFoundPaths(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})

	if err := s.UpdateSummary(ctx, 9999, "summary", "model"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateSummary on missing: %v", err)
	}
	if err := s.DeleteBoard(ctx, u.ID, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteBoard missing: %v", err)
	}
	if err := s.DeleteFilter(ctx, u.ID, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteFilter missing: %v", err)
	}
	if err := s.MarkShareSeen(ctx, u.ID, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("MarkShareSeen missing: %v", err)
	}
	if err := s.UpdateCategory(ctx, u.ID, 9999, UpdateCategoryPatch{Name: strPtr("x")}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateCategory missing: %v", err)
	}
	if err := s.UpdateFilter(ctx, u.ID, 9999, UpdateFilterPatch{Name: strPtr("x")}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateFilter missing: %v", err)
	}
	if err := s.UpdateSubscription(ctx, u.ID, 9999, UpdateSubscriptionPatch{ClearCategory: true}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateSubscription missing: %v", err)
	}
	if err := s.Unsubscribe(ctx, u.ID, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("Unsubscribe missing: %v", err)
	}
	if err := s.UpdateFeedFetch(ctx, 9999, UpdateFeedFetchPatch{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateFeedFetch missing: %v", err)
	}
}

func TestCoverage_UpdateFeedFetchAllFields(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})

	etag := `"abc"`
	lastMod := "Wed, 21 Oct 2026 07:28:00 GMT"
	title := "New Title"
	site := "https://x.test"
	favicon := "https://x.test/favicon.ico"
	if err := s.UpdateFeedFetch(ctx, f.ID, UpdateFeedFetchPatch{
		ETag: &etag, LastModified: &lastMod, Title: &title, SiteURL: &site, FaviconURL: &favicon,
		LastFetched: 1000, NextFetch: 2000, ErrorCount: 0,
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetFeed(ctx, f.ID)
	if got.ETag != etag || got.LastModified != lastMod || got.Title != title ||
		got.SiteURL != site || got.FaviconURL != favicon {
		t.Errorf("UpdateFeedFetch fields lost: %+v", got)
	}
}

func TestCoverage_ListArticles_FilterCombos(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	c, _ := s.CreateCategory(ctx, models.Category{UserID: u.ID, Name: "Tech"})
	f1, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://a.test/feed", Title: "A"})
	f2, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://b.test/feed", Title: "B"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f1.ID, CategoryID: &c.ID})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f2.ID})

	a1, _, _ := s.UpsertArticle(ctx, models.Article{FeedID: f1.ID, GUID: "g1", Title: "A1", ContentHash: "h1", PublishedAt: 1000})
	a2, _, _ := s.UpsertArticle(ctx, models.Article{FeedID: f2.ID, GUID: "g2", Title: "B1", ContentHash: "h2", PublishedAt: 2000})

	_ = s.SetStarred(ctx, u.ID, a1.ID, true)
	_ = s.SetLater(ctx, u.ID, a2.ID, true)
	_ = s.SetRead(ctx, u.ID, []int64{a1.ID}, true)

	// Filter by feed.
	if list, _ := s.ListArticles(ctx, u.ID, ListArticlesQuery{FeedID: f1.ID}); len(list) != 1 {
		t.Errorf("feed filter: %d", len(list))
	}
	// Filter by category.
	if list, _ := s.ListArticles(ctx, u.ID, ListArticlesQuery{CategoryID: c.ID}); len(list) != 1 {
		t.Errorf("category filter: %d", len(list))
	}
	// Starred view.
	if list, _ := s.ListArticles(ctx, u.ID, ListArticlesQuery{View: "starred"}); len(list) != 1 || list[0].ID != a1.ID {
		t.Errorf("starred view: %+v", list)
	}
	// Later view.
	if list, _ := s.ListArticles(ctx, u.ID, ListArticlesQuery{View: "later"}); len(list) != 1 || list[0].ID != a2.ID {
		t.Errorf("later view: %+v", list)
	}
	// Unread view.
	if list, _ := s.ListArticles(ctx, u.ID, ListArticlesQuery{View: "unread"}); len(list) != 1 || list[0].ID != a2.ID {
		t.Errorf("unread view: %+v", list)
	}
	// Fresh view with cutoff after a1 → only a2.
	if list, _ := s.ListArticles(ctx, u.ID, ListArticlesQuery{View: "fresh", FreshAfter: 1500}); len(list) != 1 || list[0].ID != a2.ID {
		t.Errorf("fresh view: %+v", list)
	}
	// Today (caller sets FreshAfter explicitly).
	if list, _ := s.ListArticles(ctx, u.ID, ListArticlesQuery{View: "today", FreshAfter: 0}); len(list) != 2 {
		t.Errorf("today view len=%d", len(list))
	}
	// Default limit kicks in when 0.
	if list, _ := s.ListArticles(ctx, u.ID, ListArticlesQuery{Limit: 0}); len(list) != 2 {
		t.Errorf("default limit: %d", len(list))
	}
	// Excessive limit is clamped.
	if list, _ := s.ListArticles(ctx, u.ID, ListArticlesQuery{Limit: 1000}); len(list) != 2 {
		t.Errorf("clamped limit: %d", len(list))
	}
}

func TestCoverage_ListArticles_BoardScope(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f.ID})
	art, _, _ := s.UpsertArticle(ctx, models.Article{FeedID: f.ID, GUID: "g1", Title: "T", ContentHash: "h1", PublishedAt: 1000})
	b, _ := s.CreateBoard(ctx, models.Board{UserID: u.ID, Name: "X"})
	_ = s.AddArticleToBoard(ctx, u.ID, b.ID, art.ID)

	list, err := s.ListArticles(ctx, u.ID, ListArticlesQuery{BoardID: b.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != art.ID {
		t.Errorf("board scope: %+v", list)
	}
}

func TestCoverage_ListArticles_SharedView(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	alice, _ := s.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "h"})
	bob, _ := s.CreateUser(ctx, models.User{Username: "bob", PasswordHash: "h"})
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: alice.ID, FeedID: f.ID})
	art, _, _ := s.UpsertArticle(ctx, models.Article{
		FeedID: f.ID, GUID: "g1", Title: "T", ContentHash: "h1", PublishedAt: 1000,
	})
	_, err := s.CreateShare(ctx, models.Share{
		ArticleID: art.ID, FromUser: alice.ID, ToUser: bob.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.ListArticles(ctx, bob.ID, ListArticlesQuery{View: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != art.ID {
		t.Errorf("shared view: %+v", list)
	}
}

func TestCoverage_CountUnreadScoped(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	c, _ := s.CreateCategory(ctx, models.Category{UserID: u.ID, Name: "C"})
	f1, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://a.test/feed", Title: "A"})
	f2, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://b.test/feed", Title: "B"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f1.ID, CategoryID: &c.ID})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f2.ID})
	_, _, _ = s.UpsertArticle(ctx, models.Article{FeedID: f1.ID, GUID: "g1", Title: "T1", ContentHash: "h1", PublishedAt: 1000})
	_, _, _ = s.UpsertArticle(ctx, models.Article{FeedID: f1.ID, GUID: "g2", Title: "T2", ContentHash: "h2", PublishedAt: 1000})
	_, _, _ = s.UpsertArticle(ctx, models.Article{FeedID: f2.ID, GUID: "g3", Title: "T3", ContentHash: "h3", PublishedAt: 1000})

	if n, _ := s.CountUnread(ctx, u.ID, f1.ID, 0); n != 2 {
		t.Errorf("by-feed unread = %d, want 2", n)
	}
	if n, _ := s.CountUnread(ctx, u.ID, 0, c.ID); n != 2 {
		t.Errorf("by-category unread = %d, want 2", n)
	}
	if n, _ := s.CountUnread(ctx, u.ID, f2.ID, 0); n != 1 {
		t.Errorf("f2 unread = %d, want 1", n)
	}
}

func TestCoverage_MarkAllRead_GlobalAndScoped(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	c, _ := s.CreateCategory(ctx, models.Category{UserID: u.ID, Name: "C"})
	f1, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://a.test/feed", Title: "A"})
	f2, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://b.test/feed", Title: "B"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f1.ID, CategoryID: &c.ID})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f2.ID})
	_, _, _ = s.UpsertArticle(ctx, models.Article{FeedID: f1.ID, GUID: "g1", Title: "T1", ContentHash: "h1", PublishedAt: 1000})
	_, _, _ = s.UpsertArticle(ctx, models.Article{FeedID: f2.ID, GUID: "g2", Title: "T2", ContentHash: "h2", PublishedAt: 2000})

	// Scope by category.
	if n, _ := s.MarkAllRead(ctx, u.ID, 0, c.ID, 0); n != 1 {
		t.Errorf("category mark-all-read = %d, want 1", n)
	}
	// Scope by fresh window.
	if n, _ := s.MarkAllRead(ctx, u.ID, 0, 0, 1500); n != 1 {
		t.Errorf("fresh mark-all-read = %d, want 1 (only published>=1500)", n)
	}
}

func TestCoverage_ListActiveFilters(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	_, _ = s.CreateFilter(ctx, models.Filter{UserID: u.ID, Name: "on", MatchJSON: "{}", Action: "x", Enabled: true})
	_, _ = s.CreateFilter(ctx, models.Filter{UserID: u.ID, Name: "off", MatchJSON: "{}", Action: "x", Enabled: false})
	active, _ := s.ListActiveFilters(ctx, u.ID)
	if len(active) != 1 || active[0].Name != "on" {
		t.Errorf("active filters: %+v", active)
	}
}

func TestCoverage_UnsubscribeNotFound(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	if err := s.Unsubscribe(ctx, u.ID, 12345); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestCoverage_UpsertArticleNoHash(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	_, feedID := seedUserAndFeed(t, s, "alice")
	_, _, err := s.UpsertArticle(ctx, models.Article{
		FeedID: feedID, GUID: "g1", Title: "T", PublishedAt: 1000,
	})
	if err == nil {
		t.Error("expected error for empty content_hash")
	}
}

func TestCoverage_FeedsDueLimitClamp(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	// Limit=0 should default; just exercise the path.
	if _, err := s.FeedsDue(ctx, 1, 0); err != nil {
		t.Fatal(err)
	}
}

func strPtr(v string) *string { return &v }
