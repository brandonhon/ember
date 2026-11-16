package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// older_days used to be unbounded: a large enough value overflowed the
// time.Duration math into a negative cutoff, and Cleanup then deleted every
// unprotected article, however new. Both the one-shot cleanup and the
// scheduled-cleanup setting take the value, so both reject it.
func TestDBCleanup_RejectsOutOfRangeOlderDays(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "root", "hunter2", true)
	cl := h.login(t, "root", "hunter2")
	ctx := context.Background()

	f, err := h.store.UpsertFeed(ctx, models.Feed{URL: "https://fresh.test/feed", Title: "Fresh"})
	if err != nil {
		t.Fatal(err)
	}
	art, _, err := h.store.UpsertArticle(ctx, models.Article{
		FeedID: f.ID, GUID: "fresh", Title: "Fresh", ContentHash: "h-fresh",
	})
	if err != nil {
		t.Fatal(err)
	}

	if code := post(t, cl, h.srv.URL+"/api/admin/db/cleanup",
		map[string]any{"older_days": 106752}, nil); code != http.StatusBadRequest {
		t.Errorf("cleanup older_days=106752 = %d, want 400", code)
	}
	if _, err := h.store.GetArticle(ctx, art.ID); err != nil {
		t.Errorf("fresh article gone after rejected cleanup: %v", err)
	}

	if code := post(t, cl, h.srv.URL+"/api/admin/db/schedule", map[string]any{
		"backup_schedule":    "off",
		"cleanup_schedule":   "weekly",
		"cleanup_older_days": 106752,
	}, nil); code != http.StatusBadRequest {
		t.Errorf("schedule cleanup_older_days=106752 = %d, want 400", code)
	}
	if v, _ := h.store.GetAppSetting(ctx, keyCleanupOlderDays); v == "106752" {
		t.Errorf("out-of-range cleanup_older_days was persisted")
	}
}

// Starring or saving an article the caller can't see must not reach the
// summary side effects: before the access check, any user could clear another
// user's 'deferred' marker and queue LLM work for an arbitrary id.
func TestSetArticleFlag_InaccessibleArticleIs404(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "p", false)
	h.seedUser(t, "bob", "p", false)
	cA := h.login(t, "alice", "p")
	cB := h.login(t, "bob", "p")
	_, feedID := addFeedFor(t, h, cA, "https://alice-only.test/feed")
	ctx := context.Background()

	art, _, err := h.store.UpsertArticle(ctx, models.Article{
		FeedID: feedID, GUID: "deferred", Title: "Deferred", ContentHash: "h-def",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.DB.ExecContext(ctx,
		`UPDATE articles SET summary_model = 'deferred' WHERE id = ?`, art.ID); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/api/articles/star", "/api/articles/later"} {
		if code := post(t, cB, h.srv.URL+path,
			map[string]any{"id": art.ID, "value": true}, nil); code != http.StatusNotFound {
			t.Errorf("bob %s on alice's article = %d, want 404", path, code)
		}
	}
	var model string
	if err := h.store.DB.QueryRowContext(ctx,
		`SELECT IFNULL(summary_model,'') FROM articles WHERE id = ?`, art.ID).Scan(&model); err != nil {
		t.Fatal(err)
	}
	if model != "deferred" {
		t.Errorf("summary_model = %q after bob's star, want deferred", model)
	}
	if got := h.enqueued(); len(got) != 0 {
		t.Errorf("enqueued %v for an article bob can't see", got)
	}

	// The owner still gets the summary request.
	if code := post(t, cA, h.srv.URL+"/api/articles/star",
		map[string]any{"id": art.ID, "value": true}, nil); code != http.StatusOK {
		t.Errorf("alice star = %d, want 200", code)
	}
	if got := h.enqueued(); len(got) != 1 || got[0] != art.ID {
		t.Errorf("enqueued after alice's star = %v, want [%d]", got, art.ID)
	}
}

// A URL change used to commit the repoint before the rest of the patch was
// checked, so a 404 on the folder still left the subscription moved.
func TestUpdateFeed_InvalidCategoryDoesNotRepoint(t *testing.T) {
	h := newHarness(t)
	uid := h.seedUser(t, "alice", "p", false).ID
	cA := h.login(t, "alice", "p")
	subID, feedID := addFeedFor(t, h, cA, "https://old.test/feed")

	if code := patch(t, cA, fmt.Sprintf("%s/api/feeds/%d", h.srv.URL, subID),
		map[string]any{"url": "https://new.test/feed", "category_id": 999999}, nil); code != http.StatusNotFound {
		t.Errorf("patch url + foreign category = %d, want 404", code)
	}
	sub, err := h.store.GetSubscriptionByID(context.Background(), uid, subID)
	if err != nil {
		t.Fatal(err)
	}
	if sub.FeedID != feedID {
		t.Errorf("subscription feed = %d after 404, want %d (repointed anyway)", sub.FeedID, feedID)
	}
}

// An empty PATCH used to be a 200 for any id, including ones that don't exist
// or belong to someone else.
func TestEmptyPatch_UnknownIDIs404(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "p", false)
	cA := h.login(t, "alice", "p")
	for _, path := range []string{"/api/feeds/999999", "/api/categories/999999"} {
		if code := patch(t, cA, h.srv.URL+path, map[string]any{}, nil); code != http.StatusNotFound {
			t.Errorf("PATCH %s {} = %d, want 404", path, code)
		}
	}
}

// The admin-settings PATCH is all-or-nothing: an invalid field must not leave
// the valid fields before it saved behind a 400.
func TestAdminSettings_InvalidFieldWritesNothing(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "root", "hunter2", true)
	cl := h.login(t, "root", "hunter2")

	if code := patch(t, cl, h.srv.URL+"/api/admin/settings",
		map[string]any{"reading_window_hours": 48, "search_window_hours": 1}, nil); code != http.StatusBadRequest {
		t.Fatalf("patch with bad search window = %d, want 400", code)
	}
	got := h.store.ResolveReadingWindowHours(context.Background(), store.DefaultReadingWindowHours)
	if got != store.DefaultReadingWindowHours {
		t.Errorf("reading_window_hours = %d after rejected patch, want %d", got, store.DefaultReadingWindowHours)
	}
}

// An admin resetting their OWN password from the Users screen must stay logged
// in on this browser, like the self-service change-password flow; their other
// sessions are still revoked.
func TestUpdateUser_OwnPasswordKeepsCurrentSession(t *testing.T) {
	h := newHarness(t)
	root := h.seedUser(t, "root", "hunter2", true)
	here := h.login(t, "root", "hunter2")
	elsewhere := h.login(t, "root", "hunter2")

	if code := patch(t, here, fmt.Sprintf("%s/api/users/%d", h.srv.URL, root.ID),
		map[string]any{"password": "new-password-1"}, nil); code != http.StatusOK {
		t.Fatalf("patch own password = %d, want 200", code)
	}
	if code := get(t, here, h.srv.URL+"/api/me", nil); code != http.StatusOK {
		t.Errorf("/me on the changing browser = %d, want 200", code)
	}
	if code := get(t, elsewhere, h.srv.URL+"/api/me", nil); code != http.StatusUnauthorized {
		t.Errorf("/me on another session = %d, want 401", code)
	}
}
