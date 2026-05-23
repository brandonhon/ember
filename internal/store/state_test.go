package store

import (
	"context"
	"testing"

	"github.com/brandonhon/ember/internal/models"
)

// TestState_CrossUserIsolation is the *critical* test: two users subscribed to
// the same feed have independent read/star/later state. Marking it read for A
// must not affect B.
func TestState_CrossUserIsolation(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()

	alice, _ := s.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "h"})
	bob, _ := s.CreateUser(ctx, models.User{Username: "bob", PasswordHash: "h"})
	feed, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: alice.ID, FeedID: feed.ID})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: bob.ID, FeedID: feed.ID})

	art, _, _ := s.UpsertArticle(ctx, models.Article{
		FeedID: feed.ID, GUID: "g1", Title: "Shared",
		ContentText: "shared body", ContentHash: "h1", PublishedAt: 1000,
	})

	// Alice marks read + starred.
	if err := s.SetRead(ctx, alice.ID, []int64{art.ID}, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStarred(ctx, alice.ID, art.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLater(ctx, alice.ID, art.ID, true); err != nil {
		t.Fatal(err)
	}

	// Bob's state is untouched.
	av, err := s.GetArticleForUser(ctx, bob.ID, art.ID)
	if err != nil {
		t.Fatal(err)
	}
	if av.IsRead || av.IsStarred || av.IsLater {
		t.Errorf("bob's state leaked: %+v", av)
	}

	// Alice sees her own state.
	av, err = s.GetArticleForUser(ctx, alice.ID, art.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !av.IsRead || !av.IsStarred || !av.IsLater {
		t.Errorf("alice's state missing: %+v", av)
	}

	// Unread counts independent.
	if n, _ := s.CountUnread(ctx, alice.ID, 0, 0); n != 0 {
		t.Errorf("alice unread = %d, want 0", n)
	}
	if n, _ := s.CountUnread(ctx, bob.ID, 0, 0); n != 1 {
		t.Errorf("bob unread = %d, want 1", n)
	}
}

func TestState_ToggleRoundTrip(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f.ID})
	art, _, _ := s.UpsertArticle(ctx, models.Article{
		FeedID: f.ID, GUID: "g1", Title: "T", ContentHash: "h1", PublishedAt: 1000,
	})

	// star → unstar.
	_ = s.SetStarred(ctx, u.ID, art.ID, true)
	st, _ := s.GetState(ctx, u.ID, art.ID)
	if !st.IsStarred {
		t.Error("not starred")
	}
	_ = s.SetStarred(ctx, u.ID, art.ID, false)
	st, _ = s.GetState(ctx, u.ID, art.ID)
	if st.IsStarred {
		t.Error("still starred")
	}

	// read → unread.
	_ = s.SetRead(ctx, u.ID, []int64{art.ID}, true)
	if n, _ := s.CountUnread(ctx, u.ID, 0, 0); n != 0 {
		t.Errorf("unread = %d", n)
	}
	_ = s.SetRead(ctx, u.ID, []int64{art.ID}, false)
	if n, _ := s.CountUnread(ctx, u.ID, 0, 0); n != 1 {
		t.Errorf("unread = %d after unread", n)
	}
}

func TestState_MarkAllReadScoped(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	f1, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://a.test/feed", Title: "A"})
	f2, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://b.test/feed", Title: "B"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f1.ID})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f2.ID})
	a1, _, _ := s.UpsertArticle(ctx, models.Article{FeedID: f1.ID, GUID: "g1", Title: "A1", ContentHash: "h1", PublishedAt: 1000})
	_, _, _ = s.UpsertArticle(ctx, models.Article{FeedID: f2.ID, GUID: "g2", Title: "B1", ContentHash: "h2", PublishedAt: 2000})

	// Mark f1 only.
	n, err := s.MarkAllRead(ctx, u.ID, f1.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("marked = %d, want 1", n)
	}
	st, _ := s.GetState(ctx, u.ID, a1.ID)
	if !st.IsRead {
		t.Error("f1's article not read")
	}
	total, _ := s.CountUnread(ctx, u.ID, 0, 0)
	if total != 1 {
		t.Errorf("total unread = %d, want 1 (only f2 remaining)", total)
	}
}
