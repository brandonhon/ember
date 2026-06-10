package store

import (
	"context"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/models"
)

// TestCountArticles_MatchesList is the core invariant behind the sidebar
// badge: a count must equal the length of the list it summarizes, across the
// shared window + summary gate + cross-feed dedup.
func TestCountArticles_MatchesList(t *testing.T) {
	s := NewTest(t)
	now := time.Unix(1_700_000_000, 0)
	s.Now = func() time.Time { return now }
	ctx := context.Background()
	userID, feedID := seedUserAndFeed(t, s, "alice")

	cutoff := now.Add(-24 * time.Hour).Unix()
	// 6 in-window unread, 2 out-of-window (older than 24h).
	for i := range 6 {
		_, _, _ = s.UpsertArticle(ctx, mkArticle(feedID,
			"in"+string(rune('a'+i)), "In "+string(rune('a'+i)), "h-in-"+string(rune('a'+i)),
			now.Add(-time.Duration(i)*time.Hour).Unix()))
	}
	for i := range 2 {
		_, _, _ = s.UpsertArticle(ctx, mkArticle(feedID,
			"old"+string(rune('a'+i)), "Old", "h-old-"+string(rune('a'+i)),
			now.Add(-48*time.Hour).Unix()))
	}

	q := ListArticlesQuery{View: "unread", FreshAfter: cutoff}
	list, err := s.ListArticles(ctx, userID, q)
	if err != nil {
		t.Fatal(err)
	}
	count, err := s.CountArticles(ctx, userID, q)
	if err != nil {
		t.Fatal(err)
	}
	if count != len(list) {
		t.Errorf("CountArticles=%d, list len=%d — must match", count, len(list))
	}
	if count != 6 {
		t.Errorf("expected 6 in-window unread, got %d", count)
	}
}

// TestUnreadCutoff_ExtendsToPreviousLogin checks the unread window anchors on
// the previous login and is clamped to [1d, retention].
func TestUnreadCutoff_ExtendsToPreviousLogin(t *testing.T) {
	s := NewTest(t)
	base := time.Unix(1_700_000_000, 0)
	now := base
	s.Now = func() time.Time { return now }
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "h"})

	// Fresh user: no prior login → floor of 24h.
	if got, want := s.UnreadCutoff(ctx, u.ID), now.Add(-24*time.Hour).Unix(); got != want {
		t.Errorf("fresh user cutoff=%d, want floor %d", got, want)
	}

	// First login, then 32h later a second login: window anchors on the first.
	if err := s.RecordLogin(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	firstLogin := now
	now = base.Add(32 * time.Hour)
	if err := s.RecordLogin(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if got, want := s.UnreadCutoff(ctx, u.ID), firstLogin.Unix(); got != want {
		t.Errorf("away-32h cutoff=%d, want previous login %d", got, want)
	}

	// A very stale prior login is clamped to the retention ceiling.
	now = base.Add(60 * 24 * time.Hour)
	if got, want := s.UnreadCutoff(ctx, u.ID), now.Add(-RetentionHours*time.Hour).Unix(); got != want {
		t.Errorf("stale cutoff=%d, want retention ceiling %d", got, want)
	}
}

// TestUnreadCutoff_FloorsAtReadingWindow checks that the reading-window setting
// raises the floor: a frequently-returning user (no useful prior-login gap)
// sees a window no narrower than the admin reading window, so the feed/category
// lists that share this cutoff honor the setting and still match their badges.
func TestUnreadCutoff_FloorsAtReadingWindow(t *testing.T) {
	s := NewTest(t)
	now := time.Unix(1_700_000_000, 0)
	s.Now = func() time.Time { return now }
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "h"})

	// Default floor is the 24h reading window.
	if got, want := s.UnreadCutoff(ctx, u.ID), now.Add(-24*time.Hour).Unix(); got != want {
		t.Errorf("default cutoff=%d, want 24h floor %d", got, want)
	}

	// Bump the reading window to 48h → the floor follows it even with a recent
	// login (prior-login gap smaller than the window).
	if err := s.PutReadingWindowHours(ctx, 48); err != nil {
		t.Fatal(err)
	}
	if got, want := s.UnreadCutoff(ctx, u.ID), now.Add(-48*time.Hour).Unix(); got != want {
		t.Errorf("48h-window cutoff=%d, want 48h floor %d", got, want)
	}
}

// TestPruneArticles_RespectsExemptions verifies the retention sweep deletes
// old articles but keeps starred / read-later ones regardless of age.
func TestPruneArticles_RespectsExemptions(t *testing.T) {
	s := NewTest(t)
	now := time.Unix(1_700_000_000, 0)
	s.Now = func() time.Time { return now }
	ctx := context.Background()
	userID, feedID := seedUserAndFeed(t, s, "alice")

	old := now.Add(-10 * 24 * time.Hour).Unix()
	recent := now.Add(-1 * time.Hour).Unix()
	oldPlain, _, _ := s.UpsertArticle(ctx, mkArticle(feedID, "oldplain", "Old", "h1", old))
	oldStar, _, _ := s.UpsertArticle(ctx, mkArticle(feedID, "oldstar", "Old starred", "h2", old))
	newPlain, _, _ := s.UpsertArticle(ctx, mkArticle(feedID, "new", "New", "h3", recent))
	if err := s.SetStarred(ctx, userID, oldStar.ID, true); err != nil {
		t.Fatal(err)
	}

	n, err := s.PruneArticles(ctx, RetentionHours*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("pruned %d, want 1 (only old non-starred)", n)
	}
	if _, err := s.GetArticle(ctx, oldPlain.ID); err == nil {
		t.Error("old plain article should be pruned")
	}
	if _, err := s.GetArticle(ctx, oldStar.ID); err != nil {
		t.Error("old starred article must survive")
	}
	if _, err := s.GetArticle(ctx, newPlain.ID); err != nil {
		t.Error("recent article must survive")
	}
}

// TestRepointSubscriptionFeed moves a subscription to a new feed, preserving
// title/category and dropping the orphaned old feed.
func TestRepointSubscriptionFeed(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "h"})
	old, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://old.test/feed", Title: "Old"})
	sub, _ := s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: old.ID, TitleOverride: "Mine"})
	newFeed, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://new.test/feed", Title: "New"})

	if err := s.RepointSubscriptionFeed(ctx, u.ID, sub.ID, newFeed.ID); err != nil {
		t.Fatalf("repoint: %v", err)
	}
	got, err := s.GetSubscriptionByID(ctx, u.ID, sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FeedID != newFeed.ID {
		t.Errorf("subscription feed_id=%d, want %d", got.FeedID, newFeed.ID)
	}
	if got.TitleOverride != "Mine" {
		t.Errorf("title override lost: %q", got.TitleOverride)
	}
	// Old feed had no other subscribers → dropped.
	if _, err := s.GetFeed(ctx, old.ID); err == nil {
		t.Error("orphaned old feed should be deleted")
	}

	// Repointing to a feed the user already has → conflict.
	dup, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://dup.test/feed", Title: "Dup"})
	sub2, _ := s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: dup.ID})
	if err := s.RepointSubscriptionFeed(ctx, u.ID, sub2.ID, newFeed.ID); err != ErrConflict {
		t.Errorf("repoint to already-subscribed feed = %v, want ErrConflict", err)
	}
}

// TestResolveWindowHours_Clamps locks the bounds on the two window settings.
func TestResolveWindowHours_Clamps(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()

	// Default when unset.
	if got := s.ResolveReadingWindowHours(ctx, DefaultReadingWindowHours); got != DefaultReadingWindowHours {
		t.Errorf("reading default=%d, want %d", got, DefaultReadingWindowHours)
	}
	// Below floor and above ceil get clamped on write+read.
	if err := s.PutReadingWindowHours(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if got := s.ResolveReadingWindowHours(ctx, DefaultReadingWindowHours); got != WindowHoursFloor {
		t.Errorf("reading below-floor=%d, want %d", got, WindowHoursFloor)
	}
	if err := s.PutSearchWindowHours(ctx, 100000); err != nil {
		t.Fatal(err)
	}
	if got := s.ResolveSearchWindowHours(ctx, DefaultSearchWindowHours); got != WindowHoursCeil {
		t.Errorf("search above-ceil=%d, want %d", got, WindowHoursCeil)
	}
}
