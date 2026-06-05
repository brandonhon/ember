package store

import (
	"context"
	"errors"
	"testing"

	"github.com/brandonhon/ember/internal/models"
)

func TestSearch_RankedHits(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	alice, _ := s.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "h"})
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: alice.ID, FeedID: f.ID})

	_, _, _ = s.UpsertArticle(ctx, models.Article{
		FeedID: f.ID, GUID: "g1", Title: "Go releases version 1.23",
		ContentText: "Go programming language new minor release", ContentHash: "h1", PublishedAt: 1000,
	})
	_, _, _ = s.UpsertArticle(ctx, models.Article{
		FeedID: f.ID, GUID: "g2", Title: "Rust 1.80 announced",
		ContentText: "Rust compiler released a new version with borrow checker improvements", ContentHash: "h2", PublishedAt: 2000,
	})
	_, _, _ = s.UpsertArticle(ctx, models.Article{
		FeedID: f.ID, GUID: "g3", Title: "Yoga at sunrise",
		ContentText: "wellness column unrelated to programming", ContentHash: "h3", PublishedAt: 3000,
	})

	hits, err := s.Search(ctx, alice.ID, "rust", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].GUID != "g2" {
		t.Errorf("expected 1 rust hit, got %+v", hits)
	}

	// Multi-term + ranking.
	hits, _ = s.Search(ctx, alice.ID, "programming", 10)
	if len(hits) != 2 {
		t.Errorf("expected 2 programming hits, got %d", len(hits))
	}
}

func TestSearch_MalformedQueryIsInvalid(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/f", Title: "X"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f.ID})

	// Each of these is a distinct FTS5 syntax failure class; all must map to
	// ErrInvalidQuery (-> 400) rather than a bare error (-> 500).
	for _, q := range []string{`"`, `(`, `NOT NOT`, `AND`, `foo:`, `*`} {
		_, err := s.Search(ctx, u.ID, q, 10)
		if !errors.Is(err, ErrInvalidQuery) {
			t.Errorf("query %q: want ErrInvalidQuery, got %v", q, err)
		}
	}

	// A valid query against an empty index is not an error.
	if _, err := s.Search(ctx, u.ID, "rust", 10); err != nil {
		t.Errorf("valid query errored: %v", err)
	}
}

func TestSearch_ScopedToSubscriptions(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	alice, _ := s.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "h"})
	bob, _ := s.CreateUser(ctx, models.User{Username: "bob", PasswordHash: "h"})

	// Alice subscribes to feed A; bob subscribes to feed B.
	fa, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://a.test/feed", Title: "A"})
	fb, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://b.test/feed", Title: "B"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: alice.ID, FeedID: fa.ID})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: bob.ID, FeedID: fb.ID})

	// Both feeds have a "rust" article.
	_, _, _ = s.UpsertArticle(ctx, models.Article{
		FeedID: fa.ID, GUID: "ga", Title: "Rust in A", ContentText: "alice's rust", ContentHash: "h1", PublishedAt: 1000,
	})
	_, _, _ = s.UpsertArticle(ctx, models.Article{
		FeedID: fb.ID, GUID: "gb", Title: "Rust in B", ContentText: "bob's rust", ContentHash: "h2", PublishedAt: 1000,
	})

	// Alice only finds her feed's article.
	hits, _ := s.Search(ctx, alice.ID, "rust", 10)
	if len(hits) != 1 || hits[0].GUID != "ga" {
		t.Errorf("alice should only see ga, got %+v", hits)
	}
	// Bob only finds his.
	hits, _ = s.Search(ctx, bob.ID, "rust", 10)
	if len(hits) != 1 || hits[0].GUID != "gb" {
		t.Errorf("bob should only see gb, got %+v", hits)
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	if hits, err := s.Search(ctx, u.ID, "", 10); err != nil || hits != nil {
		t.Errorf("empty query should yield no hits and no error, got %v %v", hits, err)
	}
}
