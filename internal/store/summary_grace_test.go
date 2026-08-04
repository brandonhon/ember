package store

import (
	"context"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/models"
)

// The summary gate used to be absolute: an article with no summary_model was
// hidden with no time bound, so slow inference (or an article dropped from a
// full summary queue) could hide it indefinitely. SummaryGraceBefore bounds it.
func TestSummaryGrace_ReleasesArticlesAfterTheWindow(t *testing.T) {
	s := NewTest(t)
	now := time.Unix(1_700_000_000, 0)
	s.Now = func() time.Time { return now }
	ctx := context.Background()
	userID, feedID := seedUserAndFeed(t, s, "alice")

	// One summarized, one still pending and fetched 5 minutes ago.
	done := mkArticle(feedID, "done", "Summarized", "h-done", now.Add(-time.Hour).Unix())
	done.Summary, done.SummaryModel = "s", "test-model"
	if _, _, err := s.UpsertArticle(ctx, done); err != nil {
		t.Fatal(err)
	}
	pending := mkArticle(feedID, "pending", "Still Pending", "h-pending", now.Add(-time.Hour).Unix())
	got, _, err := s.UpsertArticle(ctx, pending)
	if err != nil {
		t.Fatal(err)
	}
	// UpsertArticle stamps fetched_at from the store clock; make it old enough
	// to fall outside a short grace window.
	if _, err := s.DB.ExecContext(ctx, `UPDATE articles SET fetched_at = ? WHERE id = ?`,
		now.Add(-5*time.Minute).Unix(), got.ID); err != nil {
		t.Fatal(err)
	}

	base := ListArticlesQuery{View: "unread", FreshAfter: now.Add(-24 * time.Hour).Unix(), OnlySummarized: true}

	// Grace 0 (absolute gate, the old behaviour): only the summarized one.
	strict := base
	list, err := s.ListArticles(ctx, userID, strict)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Title != "Summarized" {
		t.Fatalf("absolute gate: got %d articles %v, want only the summarized one", len(list), titles(list))
	}

	// Grace window of 2 minutes: the pending article is older than that, so it
	// is released.
	graced := base
	graced.SummaryGraceBefore = now.Add(-2 * time.Minute).Unix()
	list, err = s.ListArticles(ctx, userID, graced)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Errorf("with grace: got %d articles %v, want both", len(list), titles(list))
	}

	// A pending article still INSIDE the window stays hidden — the gate must
	// still do its job in the common fast case.
	fresh := base
	fresh.SummaryGraceBefore = now.Add(-10 * time.Minute).Unix()
	list, err = s.ListArticles(ctx, userID, fresh)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("inside the window: got %d articles %v, want only the summarized one", len(list), titles(list))
	}
}

// The gate is applied in three places; if the count and the list disagree the
// sidebar badge lies about its own column. This is the invariant that has
// broken before.
func TestSummaryGrace_CountMatchesList(t *testing.T) {
	s := NewTest(t)
	now := time.Unix(1_700_000_000, 0)
	s.Now = func() time.Time { return now }
	ctx := context.Background()
	userID, feedID := seedUserAndFeed(t, s, "alice")

	for i, spec := range []struct {
		guid string
		done bool
		age  time.Duration
	}{
		{"a", true, time.Hour},
		{"b", false, 5 * time.Minute},  // pending, outside grace -> released
		{"c", false, 10 * time.Second}, // pending, inside grace  -> hidden
	} {
		a := mkArticle(feedID, spec.guid, "Article "+spec.guid, "h-"+spec.guid, now.Add(-time.Hour).Unix())
		if spec.done {
			a.Summary, a.SummaryModel = "s", "test-model"
		}
		stored, _, err := s.UpsertArticle(ctx, a)
		if err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
		if _, err := s.DB.ExecContext(ctx, `UPDATE articles SET fetched_at = ? WHERE id = ?`,
			now.Add(-spec.age).Unix(), stored.ID); err != nil {
			t.Fatal(err)
		}
	}

	grace := now.Add(-2 * time.Minute).Unix()
	for _, view := range []string{"unread", "fresh"} {
		q := ListArticlesQuery{
			View: view, FreshAfter: now.Add(-24 * time.Hour).Unix(),
			OnlySummarized: true, SummaryGraceBefore: grace, Limit: 100,
		}
		list, err := s.ListArticles(ctx, userID, q)
		if err != nil {
			t.Fatal(err)
		}
		count, err := s.CountArticles(ctx, userID, q)
		if err != nil {
			t.Fatal(err)
		}
		if count != len(list) {
			t.Errorf("%s: count=%d but list=%d %v — badge would disagree with its column",
				view, count, len(list), titles(list))
		}
		if len(list) != 2 {
			t.Errorf("%s: got %d articles %v, want the summarized one + the released pending one",
				view, len(list), titles(list))
		}
	}
}

func titles(list []models.ArticleView) []string {
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, a.Title)
	}
	return out
}

// The dedup sibling subquery must apply the SAME gate as the rows being
// listed. If it drifts, a released-by-grace lower-id copy stops suppressing
// its summarized twin and the duplicate shows up in the list — the failure
// mode the shared summaryGate helper exists to prevent.
func TestSummaryGrace_DedupSiblingUsesTheSameGate(t *testing.T) {
	s := NewTest(t)
	now := time.Unix(1_700_000_000, 0)
	s.Now = func() time.Time { return now }
	ctx := context.Background()
	userID, feedA := seedUserAndFeed(t, s, "alice")
	feedB, err := s.UpsertFeed(ctx, models.Feed{URL: "https://b.test/feed", Title: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Subscribe(ctx, models.Subscription{UserID: userID, FeedID: feedB.ID}); err != nil {
		t.Fatal(err)
	}

	// Same story in both feeds (same title fingerprint). The lower-id copy is
	// PENDING but old enough to be released by the grace window; the higher-id
	// copy is summarized.
	pendingCopy := mkArticle(feedA, "dup-a", "Shared Story", "h-dup-a", now.Add(-time.Hour).Unix())
	first, _, err := s.UpsertArticle(ctx, pendingCopy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE articles SET fetched_at = ? WHERE id = ?`,
		now.Add(-5*time.Minute).Unix(), first.ID); err != nil {
		t.Fatal(err)
	}
	summarizedCopy := mkArticle(feedB.ID, "dup-b", "Shared Story", "h-dup-b", now.Add(-time.Hour).Unix())
	summarizedCopy.Summary, summarizedCopy.SummaryModel = "s", "test-model"
	if _, _, err := s.UpsertArticle(ctx, summarizedCopy); err != nil {
		t.Fatal(err)
	}

	q := ListArticlesQuery{
		View: "unread", FreshAfter: now.Add(-24 * time.Hour).Unix(),
		OnlySummarized: true, SummaryGraceBefore: now.Add(-2 * time.Minute).Unix(), Limit: 100,
	}
	list, err := s.ListArticles(ctx, userID, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("got %d copies %v, want 1 — the released pending copy must suppress its summarized twin",
			len(list), titles(list))
	}
	count, err := s.CountArticles(ctx, userID, q)
	if err != nil {
		t.Fatal(err)
	}
	if count != len(list) {
		t.Errorf("count=%d but list=%d — badge would disagree with its column", count, len(list))
	}
}
