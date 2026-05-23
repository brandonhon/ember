package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/models"
)

// seedUserAndFeed creates a user, feed, and subscription. Returns ids.
func seedUserAndFeed(t *testing.T, s *Store, username string) (userID, feedID int64) {
	t.Helper()
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: username, PasswordHash: "h"})
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://" + username + ".test/feed", Title: username})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f.ID})
	return u.ID, f.ID
}

func mkArticle(feedID int64, guid, title, hash string, published int64) models.Article {
	return models.Article{
		FeedID:      feedID,
		GUID:        guid,
		Title:       title,
		URL:         "https://x.test/" + guid,
		ContentText: title + " body",
		ContentHash: hash,
		PublishedAt: published,
	}
}

func TestArticles_UpsertDedupByGUID(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	_, feedID := seedUserAndFeed(t, s, "alice")

	a1, ins, err := s.UpsertArticle(ctx, mkArticle(feedID, "g1", "Hello", "hash-1", 1000))
	if err != nil || !ins {
		t.Fatalf("first upsert: ins=%v err=%v", ins, err)
	}

	// Same GUID → no new row.
	a2, ins, err := s.UpsertArticle(ctx, mkArticle(feedID, "g1", "Hello rewritten", "hash-2", 1001))
	if err != nil {
		t.Fatal(err)
	}
	if ins {
		t.Error("expected dedup on guid")
	}
	if a2.ID != a1.ID {
		t.Errorf("dedup should return existing id")
	}
}

func TestArticles_UpsertDedupByContentHash(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	_, feedID := seedUserAndFeed(t, s, "alice")

	a1, ins, err := s.UpsertArticle(ctx, mkArticle(feedID, "g1", "Hello", "h-same", 1000))
	if err != nil || !ins {
		t.Fatalf("first upsert: %v", err)
	}

	// Different GUID, same content hash → dedup.
	a2, ins, err := s.UpsertArticle(ctx, mkArticle(feedID, "g2-different", "Hello", "h-same", 1001))
	if err != nil {
		t.Fatal(err)
	}
	if ins {
		t.Error("expected dedup on content_hash")
	}
	if a2.ID != a1.ID {
		t.Errorf("expected same row id")
	}
}

func TestArticles_KeysetPagination(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	userID, feedID := seedUserAndFeed(t, s, "alice")

	// Insert 10 articles with decreasing published_at.
	for i := range 10 {
		_, _, err := s.UpsertArticle(ctx, mkArticle(feedID,
			fmt.Sprintf("g%d", i),
			fmt.Sprintf("Title %d", i),
			fmt.Sprintf("h-%d", i),
			int64(2000-i)))
		if err != nil {
			t.Fatal(err)
		}
	}

	// Page 1.
	p1, err := s.ListArticles(ctx, userID, ListArticlesQuery{Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(p1) != 4 {
		t.Fatalf("page 1 len = %d", len(p1))
	}

	// Page 2 using last item's cursor.
	last := p1[len(p1)-1]
	p2, err := s.ListArticles(ctx, userID, ListArticlesQuery{
		Limit: 4, PublishedBefore: last.PublishedAt, IDBefore: last.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(p2) != 4 {
		t.Errorf("page 2 len = %d", len(p2))
	}

	// No overlap.
	seen := map[int64]bool{}
	for _, a := range p1 {
		seen[a.ID] = true
	}
	for _, a := range p2 {
		if seen[a.ID] {
			t.Errorf("article %d appears in both pages", a.ID)
		}
	}
}

func TestArticles_UnreadCount(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	userID, feedID := seedUserAndFeed(t, s, "alice")

	for i := range 5 {
		_, _, _ = s.UpsertArticle(ctx, mkArticle(feedID,
			fmt.Sprintf("g%d", i), fmt.Sprintf("T %d", i),
			fmt.Sprintf("h-%d", i), int64(1000+i)))
	}
	n, err := s.CountUnread(ctx, userID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Errorf("unread = %d, want 5", n)
	}
}

func TestArticles_GetForUserCrossUserForbidden(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	aliceID, feedID := seedUserAndFeed(t, s, "alice")
	bob, _ := s.CreateUser(ctx, models.User{Username: "bob", PasswordHash: "h"})
	// Bob is not subscribed.

	art, _, _ := s.UpsertArticle(ctx, mkArticle(feedID, "g1", "T", "h1", 1000))

	if _, err := s.GetArticleForUser(ctx, aliceID, art.ID); err != nil {
		t.Fatalf("alice should see her own article: %v", err)
	}
	if _, err := s.GetArticleForUser(ctx, bob.ID, art.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("bob should not see alice's article, got %v", err)
	}
}

func TestArticles_UpdateSummary(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	_, feedID := seedUserAndFeed(t, s, "alice")
	art, _, _ := s.UpsertArticle(ctx, mkArticle(feedID, "g1", "T", "h1", 1000))

	if err := s.UpdateSummary(ctx, art.ID, "bullet 1\nbullet 2", "qwen2.5:1.5b"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetArticle(ctx, art.ID)
	if got.Summary != "bullet 1\nbullet 2" || got.SummaryModel != "qwen2.5:1.5b" {
		t.Errorf("summary update lost: %+v", got)
	}
}

func TestArticles_FixedClock(t *testing.T) {
	s := NewTest(t)
	fixed := time.Unix(1_700_000_000, 0)
	s.Now = func() time.Time { return fixed }
	ctx := context.Background()

	u, _ := s.CreateUser(ctx, models.User{Username: "c", PasswordHash: "h"})
	if u.CreatedAt != fixed.Unix() {
		t.Errorf("clock not injected: %d != %d", u.CreatedAt, fixed.Unix())
	}
}
