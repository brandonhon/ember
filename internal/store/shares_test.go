package store

import (
	"context"
	"errors"
	"testing"

	"github.com/brandonhon/ember/internal/models"
)

func TestShares_InboxRouting(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	alice, _ := s.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "h"})
	bob, _ := s.CreateUser(ctx, models.User{Username: "bob", PasswordHash: "h"})
	carol, _ := s.CreateUser(ctx, models.User{Username: "carol", PasswordHash: "h"})

	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: alice.ID, FeedID: f.ID})
	art, _, _ := s.UpsertArticle(ctx, models.Article{
		FeedID: f.ID, GUID: "g1", Title: "T", ContentHash: "h1", PublishedAt: 1000,
	})

	sh, err := s.CreateShare(ctx, models.Share{
		ArticleID: art.ID, FromUser: alice.ID, ToUser: bob.ID, Note: "read this",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Bob has it in inbox.
	box, err := s.Inbox(ctx, bob.ID, false, 50)
	if err != nil || len(box) != 1 {
		t.Fatalf("bob inbox: %v len=%d", err, len(box))
	}
	if box[0].ID != sh.ID || box[0].Note != "read this" {
		t.Errorf("bad share: %+v", box[0])
	}

	// Alice (sender) does not.
	box, _ = s.Inbox(ctx, alice.ID, false, 50)
	if len(box) != 0 {
		t.Errorf("sender should not see own inbox: %d", len(box))
	}
	// Carol (unrelated) does not.
	box, _ = s.Inbox(ctx, carol.ID, false, 50)
	if len(box) != 0 {
		t.Errorf("unrelated user inbox should be empty: %d", len(box))
	}

	// Mark seen.
	if err := s.MarkShareSeen(ctx, bob.ID, sh.ID); err != nil {
		t.Fatal(err)
	}
	unseen, _ := s.Inbox(ctx, bob.ID, true, 50)
	if len(unseen) != 0 {
		t.Errorf("expected 0 unseen, got %d", len(unseen))
	}
}

func TestShares_SenderMustBeSubscribed(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	a, _ := s.CreateUser(ctx, models.User{Username: "a", PasswordHash: "h"})
	b, _ := s.CreateUser(ctx, models.User{Username: "b", PasswordHash: "h"})
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})
	// neither is subscribed
	art, _, _ := s.UpsertArticle(ctx, models.Article{
		FeedID: f.ID, GUID: "g1", Title: "T", ContentHash: "h1", PublishedAt: 1000,
	})

	_, err := s.CreateShare(ctx, models.Share{
		ArticleID: art.ID, FromUser: a.ID, ToUser: b.ID,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("sender must be subscribed; got %v", err)
	}
}

func TestShares_MarkSeenCrossUserForbidden(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	alice, _ := s.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "h"})
	bob, _ := s.CreateUser(ctx, models.User{Username: "bob", PasswordHash: "h"})
	carol, _ := s.CreateUser(ctx, models.User{Username: "carol", PasswordHash: "h"})
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: alice.ID, FeedID: f.ID})
	art, _, _ := s.UpsertArticle(ctx, models.Article{
		FeedID: f.ID, GUID: "g1", Title: "T", ContentHash: "h1", PublishedAt: 1000,
	})
	sh, _ := s.CreateShare(ctx, models.Share{ArticleID: art.ID, FromUser: alice.ID, ToUser: bob.ID})

	// Carol cannot mark Bob's share seen.
	if err := s.MarkShareSeen(ctx, carol.ID, sh.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
