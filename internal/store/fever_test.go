package store

import (
	"context"
	"strconv"
	"testing"

	"github.com/brandonhon/ember/internal/models"
)

func containsID(ids []int64, want int64) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// FeverItemIDs must return the COMPLETE, non-deduped unread set: a cross-feed
// duplicate (same cluster_id in a second feed) that the SPA dedups away is a
// distinct unread item in the per-feed Fever world and must still appear.
func TestFeverItemIDs_NotDeduped(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	f1, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://f1.test/feed", Title: "F1"})
	f2, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://f2.test/feed", Title: "F2"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f1.ID})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f2.ID})

	// Same URL in both feeds → same cluster_id (computed at upsert) → the SPA
	// list collapses them to one row.
	const dupURL = "https://news.test/big-story"
	a1, _, _ := s.UpsertArticle(ctx, models.Article{FeedID: f1.ID, GUID: "g1", Title: "Big Story", URL: dupURL, ContentHash: "h1", PublishedAt: 1000})
	a2, _, _ := s.UpsertArticle(ctx, models.Article{FeedID: f2.ID, GUID: "g2", Title: "Big Story", URL: dupURL, ContentHash: "h2", PublishedAt: 1001})

	// Baseline: the deduped SPA list keeps only one of the pair.
	list, _ := s.ListArticles(ctx, u.ID, ListArticlesQuery{Unread: true, Limit: 100})
	if len(list) != 1 {
		t.Fatalf("precondition: SPA list should dedup the pair to 1, got %d", len(list))
	}

	// Fever must return BOTH.
	ids, err := s.FeverItemIDs(ctx, u.ID, "unread")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || !containsID(ids, a1.ID) || !containsID(ids, a2.ID) {
		t.Fatalf("FeverItemIDs unread = %v, want both %d and %d", ids, a1.ID, a2.ID)
	}
}

// FeverItemIDs must not be capped (the old shim capped at 200).
func TestFeverItemIDs_NotCapped(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	uid, feedID := seedUserAndFeed(t, s, "alice")
	const n = 250
	for i := 0; i < n; i++ {
		guid := "g" + strconv.Itoa(i)
		_, _, err := s.UpsertArticle(ctx, mkArticle(feedID, guid, "T"+strconv.Itoa(i), "hash-"+guid, int64(1000+i)))
		if err != nil {
			t.Fatal(err)
		}
	}
	ids, err := s.FeverItemIDs(ctx, uid, "unread")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != n {
		t.Fatalf("FeverItemIDs returned %d, want all %d (no 200 cap)", len(ids), n)
	}
}

// "saved" returns starred items only; "unread" excludes read items and muted
// feeds.
func TestFeverItemIDs_FlagsReadAndMuted(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	loud, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://loud.test/feed", Title: "Loud"})
	quiet, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://quiet.test/feed", Title: "Quiet"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: loud.ID})
	subQuiet, _ := s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: quiet.ID})

	aLoud, _, _ := s.UpsertArticle(ctx, mkArticle(loud.ID, "l1", "Loud", "h-l1", 2000))
	aRead, _, _ := s.UpsertArticle(ctx, mkArticle(loud.ID, "l2", "Read", "h-l2", 2001))
	aQuiet, _, _ := s.UpsertArticle(ctx, mkArticle(quiet.ID, "q1", "Quiet", "h-q1", 2002))

	_ = s.SetRead(ctx, u.ID, []int64{aRead.ID}, true)
	_ = s.SetStarred(ctx, u.ID, aLoud.ID, true)
	muted := true
	if err := s.UpdateSubscription(ctx, u.ID, subQuiet.ID, UpdateSubscriptionPatch{Muted: &muted}); err != nil {
		t.Fatal(err)
	}

	unread, _ := s.FeverItemIDs(ctx, u.ID, "unread")
	if len(unread) != 1 || unread[0] != aLoud.ID {
		t.Fatalf("unread ids = %v, want only %d (read excluded, muted %d excluded)", unread, aLoud.ID, aQuiet.ID)
	}
	saved, _ := s.FeverItemIDs(ctx, u.ID, "saved")
	if len(saved) != 1 || saved[0] != aLoud.ID {
		t.Fatalf("saved ids = %v, want only starred %d", saved, aLoud.ID)
	}
}

// FeverItems paging: since_id walks forward, max_id backfills, with_ids is an
// explicit set, no-arg returns the most recent page, and the page is capped at
// 50. All non-deduped.
func TestFeverItems_Paging(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	uid, feedID := seedUserAndFeed(t, s, "alice")
	const n = 60
	ids := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		guid := "g" + strconv.Itoa(i)
		a, _, err := s.UpsertArticle(ctx, mkArticle(feedID, guid, "T"+strconv.Itoa(i), "hash-"+guid, int64(1000+i)))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, a.ID)
	}

	// Default page: most recent first, capped at 50.
	def, _ := s.FeverItems(ctx, uid, FeverItemQuery{})
	if len(def) != 50 {
		t.Fatalf("default page = %d, want 50 (cap)", len(def))
	}
	if def[0].ID <= def[1].ID {
		t.Fatalf("default page should be id-descending, got %d then %d", def[0].ID, def[1].ID)
	}

	// since_id: ascending ids strictly greater than the cursor.
	since := ids[0]
	fwd, _ := s.FeverItems(ctx, uid, FeverItemQuery{SinceID: since, Limit: 10})
	if len(fwd) != 10 {
		t.Fatalf("since_id page = %d, want 10", len(fwd))
	}
	if fwd[0].ID != ids[1] {
		t.Fatalf("since_id should start just after cursor: got %d, want %d", fwd[0].ID, ids[1])
	}
	for _, it := range fwd {
		if it.ID <= since {
			t.Fatalf("since_id returned id %d <= cursor %d", it.ID, since)
		}
	}

	// max_id: descending ids strictly less than the cursor.
	max := ids[n-1]
	back, _ := s.FeverItems(ctx, uid, FeverItemQuery{MaxID: max, Limit: 5})
	if len(back) != 5 || back[0].ID != ids[n-2] {
		t.Fatalf("max_id page start = %v, want first id %d", back, ids[n-2])
	}

	// with_ids: exactly the requested set.
	want := []int64{ids[3], ids[7], ids[42]}
	got, _ := s.FeverItems(ctx, uid, FeverItemQuery{WithIDs: want})
	if len(got) != 3 {
		t.Fatalf("with_ids returned %d, want 3", len(got))
	}
	for _, w := range want {
		found := false
		for _, it := range got {
			if it.ID == w {
				found = true
			}
		}
		if !found {
			t.Fatalf("with_ids missing %d (got %+v)", w, got)
		}
	}

	if total, _ := s.FeverTotalItems(ctx, uid); total != n {
		t.Fatalf("FeverTotalItems = %d, want %d", total, n)
	}
}
