package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/models"
)

func TestFeeds_UpsertDedupByURL(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	f1, err := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "First"})
	if err != nil {
		t.Fatal(err)
	}
	if f1.ID == 0 {
		t.Fatal("no id assigned")
	}
	f2, err := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	if f2.ID != f1.ID {
		t.Errorf("upsert should reuse row, got %d != %d", f2.ID, f1.ID)
	}
	// Existing title preserved (we don't overwrite via upsert).
	if f2.Title != "First" {
		t.Errorf("title = %q, want First", f2.Title)
	}
}

func TestFeeds_SubscriptionPerUser(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	a, _ := s.CreateUser(ctx, models.User{Username: "a", PasswordHash: "h"})
	b, _ := s.CreateUser(ctx, models.User{Username: "b", PasswordHash: "h"})
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})

	subA, err := s.Subscribe(ctx, models.Subscription{UserID: a.ID, FeedID: f.ID})
	if err != nil {
		t.Fatal(err)
	}
	subB, err := s.Subscribe(ctx, models.Subscription{UserID: b.ID, FeedID: f.ID})
	if err != nil {
		t.Fatal(err)
	}
	if subA.ID == subB.ID {
		t.Error("each user should get its own subscription row")
	}

	// Subscribing again is idempotent.
	subAagain, _ := s.Subscribe(ctx, models.Subscription{UserID: a.ID, FeedID: f.ID})
	if subAagain.ID != subA.ID {
		t.Errorf("idempotent re-subscribe should return same row")
	}

	// Each user sees only their own feed list.
	listA, _ := s.ListFeedsForUser(ctx, a.ID, 0, false)
	if len(listA) != 1 || listA[0].SubscriptionID != subA.ID {
		t.Errorf("A's list wrong: %+v", listA)
	}
	listB, _ := s.ListFeedsForUser(ctx, b.ID, 0, false)
	if len(listB) != 1 || listB[0].SubscriptionID != subB.ID {
		t.Errorf("B's list wrong: %+v", listB)
	}
}

// TestListFeedsForUser_UnreadCountsIncludeUnsummarized locks in that the
// sidebar's per-feed unread count (which the SPA also aggregates into the
// per-category and "All Unread" totals) reflects every unread article,
// including those the summarizer hasn't touched yet. Pairs with the parallel
// fix to the Fresh-view query in PR #45.
func TestListFeedsForUser_UnreadCountsIncludeUnsummarized(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f.ID})

	// Three articles: one finalized by the summarizer, one stamped 'disabled'
	// (EMBER_DISABLE_SUMMARIES path), one not yet touched. All three are unread.
	a1, _, _ := s.UpsertArticle(ctx, mkArticle(f.ID, "a1", "summarized", "h1", 1000))
	if err := s.UpdateSummary(ctx, a1.ID, "bullet", "qwen2.5:0.5b"); err != nil {
		t.Fatal(err)
	}
	a2, _, _ := s.UpsertArticle(ctx, mkArticle(f.ID, "a2", "disabled", "h2", 1500))
	if err := s.UpdateSummary(ctx, a2.ID, "", "disabled"); err != nil {
		t.Fatal(err)
	}
	_, _, _ = s.UpsertArticle(ctx, mkArticle(f.ID, "a3", "pending", "h3", 2000))

	list, err := s.ListFeedsForUser(ctx, u.ID, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 feed, got %d", len(list))
	}
	if list[0].Unread != 3 {
		t.Errorf("unread should include un-summarized articles: got %d, want 3", list[0].Unread)
	}
}

// TestListFeedsForUser_UnreadExcludesMuted locks in that the per-feed unread
// count returned for a muted subscription is zero. The client sums these into
// the "All Unread" badge, so unread items on a feed the user has explicitly
// muted should not inflate that count.
func TestListFeedsForUser_UnreadExcludesMuted(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	loud, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://loud.test/feed", Title: "Loud"})
	quiet, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://quiet.test/feed", Title: "Quiet"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: loud.ID})
	subQuiet, _ := s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: quiet.ID})

	// Two unread articles per feed.
	_, _, _ = s.UpsertArticle(ctx, mkArticle(loud.ID, "l1", "l-1", "hl1", 1000))
	_, _, _ = s.UpsertArticle(ctx, mkArticle(loud.ID, "l2", "l-2", "hl2", 1500))
	_, _, _ = s.UpsertArticle(ctx, mkArticle(quiet.ID, "q1", "q-1", "hq1", 1000))
	_, _, _ = s.UpsertArticle(ctx, mkArticle(quiet.ID, "q2", "q-2", "hq2", 1500))

	// Mute the quiet subscription.
	mute := true
	if err := s.UpdateSubscription(ctx, u.ID, subQuiet.ID, UpdateSubscriptionPatch{Muted: &mute}); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListFeedsForUser(ctx, u.ID, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	gotByURL := map[string]int{}
	for _, f := range list {
		gotByURL[f.URL] = f.Unread
	}
	if gotByURL["https://loud.test/feed"] != 2 {
		t.Errorf("loud (unmuted) unread = %d, want 2", gotByURL["https://loud.test/feed"])
	}
	if gotByURL["https://quiet.test/feed"] != 0 {
		t.Errorf("quiet (muted) unread = %d, want 0 (muted feeds must not count)", gotByURL["https://quiet.test/feed"])
	}
}

func TestFeeds_UnsubscribeKeepsFeedWhenOthersSubscribed(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	a, _ := s.CreateUser(ctx, models.User{Username: "a", PasswordHash: "h"})
	b, _ := s.CreateUser(ctx, models.User{Username: "b", PasswordHash: "h"})
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})
	subA, _ := s.Subscribe(ctx, models.Subscription{UserID: a.ID, FeedID: f.ID})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: b.ID, FeedID: f.ID})

	if err := s.Unsubscribe(ctx, a.ID, subA.ID); err != nil {
		t.Fatal(err)
	}
	// Feed should still exist.
	if _, err := s.GetFeed(ctx, f.ID); err != nil {
		t.Errorf("feed should survive: %v", err)
	}
}

func TestFeeds_UnsubscribeDropsFeedWhenLast(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	a, _ := s.CreateUser(ctx, models.User{Username: "a", PasswordHash: "h"})
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})
	subA, _ := s.Subscribe(ctx, models.Subscription{UserID: a.ID, FeedID: f.ID})

	if err := s.Unsubscribe(ctx, a.ID, subA.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetFeed(ctx, f.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("feed should be deleted, got %v", err)
	}
}

func TestFeeds_UnsubscribeCrossUserForbidden(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	a, _ := s.CreateUser(ctx, models.User{Username: "a", PasswordHash: "h"})
	b, _ := s.CreateUser(ctx, models.User{Username: "b", PasswordHash: "h"})
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})
	subA, _ := s.Subscribe(ctx, models.Subscription{UserID: a.ID, FeedID: f.ID})

	// B tries to unsubscribe A's subscription by ID.
	if err := s.Unsubscribe(ctx, b.ID, subA.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-user unsubscribe should be ErrNotFound, got %v", err)
	}
}

func TestFeeds_UpdateSubscriptionCategory(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	c, _ := s.CreateCategory(ctx, models.Category{UserID: u.ID, Name: "C"})
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})
	sub, _ := s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f.ID})

	if err := s.UpdateSubscription(ctx, u.ID, sub.ID, UpdateSubscriptionPatch{CategoryID: &c.ID}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSubscriptionByID(ctx, u.ID, sub.ID)
	if got.CategoryID == nil || *got.CategoryID != c.ID {
		t.Errorf("category not set: %+v", got)
	}

	if err := s.UpdateSubscription(ctx, u.ID, sub.ID, UpdateSubscriptionPatch{ClearCategory: true}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetSubscriptionByID(ctx, u.ID, sub.ID)
	if got.CategoryID != nil {
		t.Errorf("category should be cleared")
	}
}

func TestFeeds_UpdateSubscriptionRejectsForeignCategory(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	alice, _ := s.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "h"})
	bob, _ := s.CreateUser(ctx, models.User{Username: "bob", PasswordHash: "h"})
	bobCat, _ := s.CreateCategory(ctx, models.Category{UserID: bob.ID, Name: "Bob"})
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})
	sub, _ := s.Subscribe(ctx, models.Subscription{UserID: alice.ID, FeedID: f.ID})

	// Alice cannot file her subscription under Bob's category id.
	if err := s.UpdateSubscription(ctx, alice.ID, sub.ID, UpdateSubscriptionPatch{CategoryID: &bobCat.ID}); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign category = %v, want ErrNotFound", err)
	}
	got, _ := s.GetSubscriptionByID(ctx, alice.ID, sub.ID)
	if got.CategoryID != nil {
		t.Errorf("subscription category should be unchanged: %+v", got)
	}
}

func TestFeeds_DueAndFetchUpdate(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})

	// Never-fetched feed counts as due.
	due, err := s.FeedsDue(ctx, time.Now().Unix(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != f.ID {
		t.Errorf("expected feed due, got %d", len(due))
	}

	now := time.Now().Unix()
	next := now + 3600
	etag := `"abc"`
	if err := s.UpdateFeedFetch(ctx, f.ID, UpdateFeedFetchPatch{
		LastFetched: now, NextFetch: next, ErrorCount: 0, ETag: &etag,
	}); err != nil {
		t.Fatal(err)
	}

	// No longer due.
	due, _ = s.FeedsDue(ctx, now, 10)
	if len(due) != 0 {
		t.Errorf("expected 0 due, got %d", len(due))
	}

	got, _ := s.GetFeed(ctx, f.ID)
	if got.ETag != `"abc"` || got.NextFetch != next {
		t.Errorf("fetch update lost: %+v", got)
	}
}
