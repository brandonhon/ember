package store

import (
	"context"
	"errors"
	"testing"

	"github.com/brandonhon/ember/internal/models"
)

func TestBoards_CRUD(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})

	b, err := s.CreateBoard(ctx, models.Board{UserID: u.ID, Name: "Reading"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetBoard(ctx, u.ID, b.ID)
	if err != nil || got.Name != "Reading" {
		t.Fatalf("get: %v %+v", err, got)
	}
	list, _ := s.ListBoards(ctx, u.ID)
	if len(list) != 1 {
		t.Errorf("list len = %d", len(list))
	}

	// Duplicate name → conflict.
	if _, err := s.CreateBoard(ctx, models.Board{UserID: u.ID, Name: "Reading"}); !errors.Is(err, ErrConflict) {
		t.Errorf("expected conflict, got %v", err)
	}

	if err := s.DeleteBoard(ctx, u.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetBoard(ctx, u.ID, b.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected not found, got %v", err)
	}
}

func TestBoards_AddArticleRequiresSubscription(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	alice, _ := s.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "h"})
	bob, _ := s.CreateUser(ctx, models.User{Username: "bob", PasswordHash: "h"})
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: alice.ID, FeedID: f.ID})
	art, _, _ := s.UpsertArticle(ctx, models.Article{
		FeedID: f.ID, GUID: "g1", Title: "T", ContentHash: "h1", PublishedAt: 1000,
	})

	aliceBoard, _ := s.CreateBoard(ctx, models.Board{UserID: alice.ID, Name: "X"})
	bobBoard, _ := s.CreateBoard(ctx, models.Board{UserID: bob.ID, Name: "X"})

	// Alice (subscribed) can add.
	if err := s.AddArticleToBoard(ctx, alice.ID, aliceBoard.ID, art.ID); err != nil {
		t.Errorf("alice add: %v", err)
	}
	// Bob (not subscribed) cannot.
	if err := s.AddArticleToBoard(ctx, bob.ID, bobBoard.ID, art.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("bob add should be ErrNotFound, got %v", err)
	}
	// Bob cannot add to alice's board either.
	if err := s.AddArticleToBoard(ctx, bob.ID, aliceBoard.ID, art.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-user board add should be ErrNotFound, got %v", err)
	}

	if err := s.RemoveArticleFromBoard(ctx, alice.ID, aliceBoard.ID, art.ID); err != nil {
		t.Errorf("remove: %v", err)
	}
	if err := s.RemoveArticleFromBoard(ctx, alice.ID, aliceBoard.ID, art.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("remove twice should be ErrNotFound, got %v", err)
	}
}
