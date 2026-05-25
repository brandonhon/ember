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

func TestArticles_HiddenUntilSummarized(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	userID, feedID := seedUserAndFeed(t, s, "alice")

	// Insert two articles; only one gets a summary_model.
	_, _, _ = s.UpsertArticle(ctx, mkArticle(feedID, "g1", "Pending LLM", "h1", 1000))
	a2, _, _ := s.UpsertArticle(ctx, mkArticle(feedID, "g2", "Already summarized", "h2", 2000))
	if err := s.UpdateSummary(ctx, a2.ID, "• bullet", "noop"); err != nil {
		t.Fatal(err)
	}

	// Default list returns both (CountUnread is admin-perspective).
	all, _ := s.ListArticles(ctx, userID, ListArticlesQuery{})
	if len(all) != 2 {
		t.Errorf("default list returns both, got %d", len(all))
	}

	// OnlySummarized=true hides the pending one — what the SPA passes.
	list, _ := s.ListArticles(ctx, userID, ListArticlesQuery{OnlySummarized: true})
	if len(list) != 1 || list[0].ID != a2.ID {
		t.Errorf("OnlySummarized list should only show summarized article, got %+v", list)
	}

	// CountUnreadVisible matches that view.
	if n, _ := s.CountUnreadVisible(ctx, userID, 0, 0); n != 1 {
		t.Errorf("CountUnreadVisible should skip unsummarized, got %d", n)
	}
	// CountUnread (admin) sees both.
	if n, _ := s.CountUnread(ctx, userID, 0, 0); n != 2 {
		t.Errorf("CountUnread should see both, got %d", n)
	}
}

func TestArticles_SkippedMarkerStillVisible(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	userID, feedID := seedUserAndFeed(t, s, "alice")
	a, _, _ := s.UpsertArticle(ctx, mkArticle(feedID, "g1", "T", "h1", 1000))
	// The poller writes 'skipped' when the LLM fails. The article must still
	// be visible in the list.
	if err := s.UpdateSummary(ctx, a.ID, "", "skipped"); err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListArticles(ctx, userID, ListArticlesQuery{OnlySummarized: true})
	if len(list) != 1 {
		t.Errorf("skipped article should be visible, got %d", len(list))
	}
	if list[0].Summary != "" {
		t.Errorf("skipped article should have no summary text, got %q", list[0].Summary)
	}
}

func TestArticles_MutedFeedHiddenFromSmartViews(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	loud, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://loud.test/feed", Title: "Loud"})
	quiet, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://quiet.test/feed", Title: "Quiet"})
	subLoud, _ := s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: loud.ID})
	subQuiet, _ := s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: quiet.ID})

	// One summarized article from each feed.
	a1, _, _ := s.UpsertArticle(ctx, mkArticle(loud.ID, "l1", "Loud one", "h-l1", 2000))
	a2, _, _ := s.UpsertArticle(ctx, mkArticle(quiet.ID, "q1", "Quiet one", "h-q1", 2000))
	_ = s.UpdateSummary(ctx, a1.ID, "• loud", "noop")
	_ = s.UpdateSummary(ctx, a2.ID, "• quiet", "noop")

	// Mute the quiet feed.
	muted := true
	if err := s.UpdateSubscription(ctx, u.ID, subQuiet.ID, UpdateSubscriptionPatch{Muted: &muted}); err != nil {
		t.Fatal(err)
	}

	// Smart view (unread) excludes muted feeds.
	smart, _ := s.ListArticles(ctx, u.ID, ListArticlesQuery{View: "unread", OnlySummarized: true})
	if len(smart) != 1 || smart[0].FeedID != loud.ID {
		t.Errorf("smart view should hide muted; got %+v", smart)
	}

	// Aggregate unread count drops to 1.
	if n, _ := s.CountUnreadVisible(ctx, u.ID, 0, 0); n != 1 {
		t.Errorf("aggregate unread = %d, want 1", n)
	}

	// Per-feed listing still works (user clicked the muted feed directly).
	direct, _ := s.ListArticles(ctx, u.ID, ListArticlesQuery{FeedID: quiet.ID, OnlySummarized: true})
	if len(direct) != 1 || direct[0].FeedID != quiet.ID {
		t.Errorf("muted feed still accessible by FeedID; got %+v", direct)
	}

	// Per-feed badge count for the muted feed is still computed.
	if n, _ := s.CountUnreadVisible(ctx, u.ID, quiet.ID, 0); n != 1 {
		t.Errorf("per-feed unread for muted feed = %d, want 1", n)
	}

	// Unmute and the smart view shows both.
	unmute := false
	_ = s.UpdateSubscription(ctx, u.ID, subQuiet.ID, UpdateSubscriptionPatch{Muted: &unmute})
	smart, _ = s.ListArticles(ctx, u.ID, ListArticlesQuery{View: "unread", OnlySummarized: true})
	if len(smart) != 2 {
		t.Errorf("after unmute smart view has %d, want 2", len(smart))
	}

	_ = subLoud
}

func TestArticles_CrossFeedDedup(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	hn, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://hn.test/feed", Title: "HN"})
	tc, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://tc.test/feed", Title: "TC"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: hn.ID})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: tc.ID})

	// Both feeds publish the same article URL (HN linked to a TC story).
	shared := "https://example.com/big-story"
	a1 := models.Article{FeedID: hn.ID, GUID: "hn-1", URL: shared, Title: "HN's take", ContentText: "x", ContentHash: "h-hn", PublishedAt: 2000}
	a2 := models.Article{FeedID: tc.ID, GUID: "tc-1", URL: shared, Title: "TC original", ContentText: "x", ContentHash: "h-tc", PublishedAt: 2001}
	r1, _, _ := s.UpsertArticle(ctx, a1)
	r2, _, _ := s.UpsertArticle(ctx, a2)
	_ = s.UpdateSummary(ctx, r1.ID, "• one", "noop")
	_ = s.UpdateSummary(ctx, r2.ID, "• two", "noop")

	// Smart view: only the earlier-id row appears (the HN one in this case).
	list, _ := s.ListArticles(ctx, u.ID, ListArticlesQuery{View: "fresh", FreshAfter: 1, OnlySummarized: true})
	if len(list) != 1 {
		t.Fatalf("smart view should dedup by url; got %d rows: %+v", len(list), list)
	}
	if list[0].ID != r1.ID {
		t.Errorf("dedup should keep lowest id (r1=%d), got %d", r1.ID, list[0].ID)
	}
	if list[0].DupCount != 1 {
		t.Errorf("dup_count = %d, want 1 (other feed has the same url)", list[0].DupCount)
	}

	// Per-feed view still shows the article from that specific feed.
	direct, _ := s.ListArticles(ctx, u.ID, ListArticlesQuery{FeedID: tc.ID, OnlySummarized: true})
	if len(direct) != 1 || direct[0].ID != r2.ID {
		t.Errorf("per-feed view should show its own row; got %+v", direct)
	}
}

func TestArticles_ResetSummariesByFeed(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	_, feedID := seedUserAndFeed(t, s, "alice")

	a1, _, _ := s.UpsertArticle(ctx, mkArticle(feedID, "g1", "Skipped", "h1", 1000))
	a2, _, _ := s.UpsertArticle(ctx, mkArticle(feedID, "g2", "Real summary", "h2", 1000))
	a3, _, _ := s.UpsertArticle(ctx, mkArticle(feedID, "g3", "Still pending", "h3", 1000))

	_ = s.UpdateSummary(ctx, a1.ID, "", "skipped")
	_ = s.UpdateSummary(ctx, a2.ID, "• real summary", "qwen2.5:1.5b")

	ids, err := s.ResetSummariesByFeed(ctx, feedID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != a1.ID {
		t.Errorf("ResetSummariesByFeed returned %v, want [%d]", ids, a1.ID)
	}

	got, _ := s.GetArticle(ctx, a1.ID)
	if got.SummaryModel != "" {
		t.Errorf("a1 summary_model = %q, want empty", got.SummaryModel)
	}
	got, _ = s.GetArticle(ctx, a2.ID)
	if got.SummaryModel != "qwen2.5:1.5b" {
		t.Errorf("a2 summary_model changed to %q", got.SummaryModel)
	}
	got, _ = s.GetArticle(ctx, a3.ID)
	if got.SummaryModel != "" {
		t.Errorf("a3 should still be NULL, got %q", got.SummaryModel)
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
