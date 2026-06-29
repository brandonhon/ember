package store

import (
	"context"
	"testing"

	"github.com/brandonhon/ember/internal/models"
)

// Cross-feed dedup must match each suppressor sibling against the SAME
// unread/window predicate as the rows being counted. The production bug: a
// story's lowest-id copy was already-read (and/or outside the reading window),
// so it "won" the dedup but was filtered out — silently zeroing every visible
// unread copy and collapsing Fresh/All-Unread to 0 while per-feed badges still
// had counts.
func TestDedup_ReadLowerCopyDoesNotZeroUnread(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	f1, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://f1.test/feed", Title: "F1"})
	f2, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://f2.test/feed", Title: "F2"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f1.ID})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f2.ID})

	const dupURL = "https://news.test/big-story"
	// a1 (lower id, feed f1) and a2 (higher id, feed f2): same URL -> same cluster_id.
	a1, _, _ := s.UpsertArticle(ctx, models.Article{FeedID: f1.ID, GUID: "g1", Title: "Big Story", URL: dupURL, ContentHash: "h1", PublishedAt: 1000})
	_, _, _ = s.UpsertArticle(ctx, models.Article{FeedID: f2.ID, GUID: "g2", Title: "Big Story", URL: dupURL, ContentHash: "h2", PublishedAt: 1001})

	// Read the LOWER-id copy. The unread copy in f2 must still count.
	if err := s.SetRead(ctx, u.ID, []int64{a1.ID}, true); err != nil {
		t.Fatal(err)
	}

	n, err := s.CountArticles(ctx, u.ID, ListArticlesQuery{View: "unread"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("All-Unread = %d, want 1 (read lower-id copy must not suppress the unread copy)", n)
	}
}

// "Mark all read" of the visible (deduped) cards must also clear their hidden
// cross-feed siblings, otherwise the suppressor — which only hides UNREAD copies
// — lets the sibling resurface the instant its winner is read. MarkReadWithSiblings
// sweeps the whole cluster so the unread view truly empties; a non-duplicate
// unread story in another feed is left alone.
func TestMarkReadWithSiblings_ClearsResurfacingDuplicate(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	f1, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://f1.test/feed", Title: "F1"})
	f2, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://f2.test/feed", Title: "F2"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f1.ID})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f2.ID})

	const dupURL = "https://news.test/big-story"
	a1, _, _ := s.UpsertArticle(ctx, models.Article{FeedID: f1.ID, GUID: "g1", Title: "Big Story", URL: dupURL, ContentHash: "h1", PublishedAt: 1000})
	_, _, _ = s.UpsertArticle(ctx, models.Article{FeedID: f2.ID, GUID: "g2", Title: "Big Story", URL: dupURL, ContentHash: "h2", PublishedAt: 1001})
	// A non-duplicate unread story must survive the sweep.
	_, _, _ = s.UpsertArticle(ctx, models.Article{FeedID: f2.ID, GUID: "g3", Title: "Only In F2", URL: "https://f2.test/uniq", ContentHash: "h3", PublishedAt: 1002})

	// Mark the visible winner (a1) read with siblings — the f2 copy must go too.
	if err := s.MarkReadWithSiblings(ctx, u.ID, []int64{a1.ID}); err != nil {
		t.Fatal(err)
	}

	n, err := s.CountArticles(ctx, u.ID, ListArticlesQuery{View: "unread"})
	if err != nil {
		t.Fatal(err)
	}
	// Only the unique story remains; the duplicate (both copies) is fully read.
	if n != 1 {
		t.Fatalf("All-Unread = %d, want 1 (duplicate cleared incl. sibling; unique story kept)", n)
	}
}

// The suppressor must also respect the view's reading WINDOW, not just the read
// flag. A duplicate's lowest-id copy can sit outside the window (older than the
// cutoff) — common with a 24h reading window and a story re-run days later. Pre-
// fix that out-of-window copy still "won" dedup and suppressed the in-window
// unread copy, zeroing the count. The suppressor now carries the same FreshAfter
// clause as the list, so an out-of-window copy can't suppress an in-window one.
func TestDedup_OutOfWindowLowerCopyDoesNotZeroUnread(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	f1, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://f1.test/feed", Title: "F1"})
	f2, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://f2.test/feed", Title: "F2"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f1.ID})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f2.ID})

	const dupURL = "https://news.test/big-story"
	// Lower id (f1) is OLD/out-of-window; higher id (f2) is recent/in-window.
	_, _, _ = s.UpsertArticle(ctx, models.Article{FeedID: f1.ID, GUID: "g1", Title: "Big Story", URL: dupURL, ContentHash: "h1", PublishedAt: 1000})
	_, _, _ = s.UpsertArticle(ctx, models.Article{FeedID: f2.ID, GUID: "g2", Title: "Big Story", URL: dupURL, ContentHash: "h2", PublishedAt: 5000})

	// Window cutoff sits between the two copies: only the higher-id copy is in.
	n, err := s.CountArticles(ctx, u.ID, ListArticlesQuery{View: "unread", FreshAfter: 4000})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("All-Unread = %d, want 1 (out-of-window lower-id copy must not suppress the in-window unread copy)", n)
	}
}

// A duplicate ingested AFTER the cluster was read must inherit the read state at
// ingest, not resurface as a fresh unread card. The list suppressor only hides a
// copy when a lower-id sibling is itself unread, so an already-read winner can't
// hide a late arrival — UpsertArticle marks the new row read if any sibling was
// already read (the ingest-side mirror of MarkReadWithSiblings). Covers both a
// late cross-feed copy and a same-feed re-publish under a new guid+content_hash.
func TestUpsert_LateDuplicateInheritsClusterRead(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	f1, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://f1.test/feed", Title: "F1"})
	f2, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://f2.test/feed", Title: "F2"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f1.ID})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f2.ID})

	const dupURL = "https://news.test/big-story"
	// Read the story in f1.
	a1, _, _ := s.UpsertArticle(ctx, models.Article{FeedID: f1.ID, GUID: "g1", Title: "Big Story", URL: dupURL, ContentHash: "h1", PublishedAt: 1000})
	if err := s.SetRead(ctx, u.ID, []int64{a1.ID}, true); err != nil {
		t.Fatal(err)
	}

	// LATER: f2 publishes the same story (same URL → same cluster_id).
	a2, ins, err := s.UpsertArticle(ctx, models.Article{FeedID: f2.ID, GUID: "g2", Title: "Big Story", URL: dupURL, ContentHash: "h2", PublishedAt: 1001})
	if err != nil || !ins {
		t.Fatalf("insert f2 copy: ins=%v err=%v", ins, err)
	}
	av, err := s.GetArticleForUser(ctx, u.ID, a2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !av.IsRead {
		t.Fatalf("late cross-feed duplicate came in unread; want inherited read")
	}

	// Same-feed re-publish (fresh guid + content_hash, same URL) must also inherit.
	a3, ins, err := s.UpsertArticle(ctx, models.Article{FeedID: f1.ID, GUID: "g1-v2", Title: "Big Story", URL: dupURL, ContentHash: "h3", PublishedAt: 1002})
	if err != nil || !ins {
		t.Fatalf("insert republish: ins=%v err=%v", ins, err)
	}
	av3, _ := s.GetArticleForUser(ctx, u.ID, a3.ID)
	if !av3.IsRead {
		t.Fatalf("same-feed re-publish came in unread; want inherited read")
	}

	// Net effect: the read story's duplicates never resurface in Unread.
	n, err := s.CountArticles(ctx, u.ID, ListArticlesQuery{View: "unread"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("All-Unread = %d, want 0 (a read story's late duplicates must stay read)", n)
	}
}

// An UNREAD cluster must NOT be affected by ingest inheritance: a new copy of a
// story the user hasn't read yet stays unread (and dedups normally), so this
// can't silently swallow genuinely-new stories.
func TestUpsert_LateDuplicateOfUnreadClusterStaysUnread(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	f1, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://f1.test/feed", Title: "F1"})
	f2, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://f2.test/feed", Title: "F2"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f1.ID})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f2.ID})

	const dupURL = "https://news.test/unread-story"
	_, _, _ = s.UpsertArticle(ctx, models.Article{FeedID: f1.ID, GUID: "g1", Title: "Unread Story", URL: dupURL, ContentHash: "h1", PublishedAt: 1000})
	a2, _, _ := s.UpsertArticle(ctx, models.Article{FeedID: f2.ID, GUID: "g2", Title: "Unread Story", URL: dupURL, ContentHash: "h2", PublishedAt: 1001})

	av, _ := s.GetArticleForUser(ctx, u.ID, a2.ID)
	if av.IsRead {
		t.Fatalf("duplicate of an UNREAD cluster was marked read; inheritance must require a read sibling")
	}
	n, _ := s.CountArticles(ctx, u.ID, ListArticlesQuery{View: "unread"})
	if n != 1 {
		t.Fatalf("All-Unread = %d, want 1 (unread story counted once)", n)
	}
}

// Full per-feed dedup: a duplicated unread story is counted once, owned by the
// lowest-id (first-ingested) feed. The per-feed deduped badges must sum to the
// All-Unread count, and opening the "loser" feed must not show the duplicate.
func TestDedup_PerFeedCountsAndListAreConsistent(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	f1, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://f1.test/feed", Title: "F1"})
	f2, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://f2.test/feed", Title: "F2"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f1.ID})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f2.ID})

	const dupURL = "https://news.test/big-story"
	a1, _, _ := s.UpsertArticle(ctx, models.Article{FeedID: f1.ID, GUID: "g1", Title: "Big Story", URL: dupURL, ContentHash: "h1", PublishedAt: 1000})
	a2, _, _ := s.UpsertArticle(ctx, models.Article{FeedID: f2.ID, GUID: "g2", Title: "Big Story", URL: dupURL, ContentHash: "h2", PublishedAt: 1001})
	// A non-duplicate unread story only in f2, so f2 still has its own count.
	uniq, _, _ := s.UpsertArticle(ctx, models.Article{FeedID: f2.ID, GUID: "g3", Title: "Only In F2", URL: "https://f2.test/uniq", ContentHash: "h3", PublishedAt: 1002})

	// Per-feed deduped unread: f1 owns the dup (lowest id); f2 keeps only its unique story.
	byFeed, err := s.CountUnreadByFeed(ctx, u.ID, ListArticlesQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if byFeed[f1.ID] != 1 {
		t.Errorf("f1 unread = %d, want 1 (owns the dup)", byFeed[f1.ID])
	}
	if byFeed[f2.ID] != 1 {
		t.Errorf("f2 unread = %d, want 1 (its unique story only; dup suppressed)", byFeed[f2.ID])
	}

	// Badges must sum to All-Unread.
	all, _ := s.CountArticles(ctx, u.ID, ListArticlesQuery{View: "unread"})
	sum := byFeed[f1.ID] + byFeed[f2.ID]
	if sum != all {
		t.Errorf("per-feed sum %d != All-Unread %d", sum, all)
	}
	if all != 2 {
		t.Errorf("All-Unread = %d, want 2 (one deduped story + one unique)", all)
	}

	// Opening the loser feed (f2) must hide the duplicate but keep the unique story.
	list, err := s.ListArticles(ctx, u.ID, ListArticlesQuery{FeedID: f2.ID, DedupUnread: true, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != uniq.ID {
		t.Fatalf("f2 list = %v, want only the unique story id %d (dup id %d suppressed)", idsOf(list), uniq.ID, a2.ID)
	}

	// Opening the winner feed (f1) shows the dup.
	list1, _ := s.ListArticles(ctx, u.ID, ListArticlesQuery{FeedID: f1.ID, DedupUnread: true, Limit: 50})
	if len(list1) != 1 || list1[0].ID != a1.ID {
		t.Fatalf("f1 list = %v, want the dup id %d", idsOf(list1), a1.ID)
	}
}

func idsOf(as []models.ArticleView) []int64 {
	out := make([]int64, len(as))
	for i, a := range as {
		out[i] = a.ID
	}
	return out
}
