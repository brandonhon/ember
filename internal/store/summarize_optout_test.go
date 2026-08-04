package store

import (
	"context"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/models"
)

// seedTwoSubscribers gives alice and bob the same feed, with one shared
// summarized article. The article row is shared — that is the whole reason
// opting out has to blank at serve time rather than clear the column.
func seedTwoSubscribers(t *testing.T, s *Store) (alice, bob, feedID, articleID int64) {
	t.Helper()
	ctx := context.Background()
	a, err := s.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateUser(ctx, models.User{Username: "bob", PasswordHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := s.UpsertFeed(ctx, models.Feed{URL: "https://shared.test/feed", Title: "Shared"})
	if err != nil {
		t.Fatal(err)
	}
	for _, uid := range []int64{a.ID, b.ID} {
		if _, err := s.Subscribe(ctx, models.Subscription{UserID: uid, FeedID: f.ID}); err != nil {
			t.Fatal(err)
		}
	}
	art := mkArticle(f.ID, "shared-1", "Shared Story", "h-shared-1", s.Now().Add(-time.Hour).Unix())
	art.Summary = "the model's summary"
	art.SummaryModel = "test-model"
	stored, _, err := s.UpsertArticle(ctx, art)
	if err != nil {
		t.Fatal(err)
	}
	return a.ID, b.ID, f.ID, stored.ID
}

func setSummarize(t *testing.T, s *Store, userID, feedID int64, on bool) {
	t.Helper()
	sub, err := s.GetSubscription(context.Background(), userID, feedID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSubscription(context.Background(), userID, sub.ID,
		UpdateSubscriptionPatch{Summarize: &on}); err != nil {
		t.Fatal(err)
	}
}

// Suppression must be UNANIMOUS — one subscriber wanting summaries keeps the
// feed summarized, because the summary is shared.
func TestFeedSummariesSuppressed_RequiresUnanimity(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	alice, bob, feedID, _ := seedTwoSubscribers(t, s)

	if got, err := s.FeedSummariesSuppressed(ctx, feedID); err != nil || got {
		t.Fatalf("default = %v (err %v), want false — subscriptions default to summarize=1", got, err)
	}
	setSummarize(t, s, alice, feedID, false)
	if got, _ := s.FeedSummariesSuppressed(ctx, feedID); got {
		t.Error("one opt-out suppressed the feed — bob still wants summaries")
	}
	setSummarize(t, s, bob, feedID, false)
	if got, _ := s.FeedSummariesSuppressed(ctx, feedID); !got {
		t.Error("all subscribers opted out but the feed is not suppressed")
	}
}

// A feed with no subscribers must FAIL OPEN. handleAddFeed does
// UpsertFeed -> Subscribe -> RefreshFeed, and a feed with next_fetch NULL is
// immediately due, so a concurrent tick can ingest before the first
// subscription exists. Treating that as "nobody wants it" would permanently
// stamp those articles excluded.
func TestFeedSummariesSuppressed_ZeroSubscribersFailsOpen(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	f, err := s.UpsertFeed(ctx, models.Feed{URL: "https://orphan.test/feed", Title: "Orphan"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.FeedSummariesSuppressed(ctx, f.ID); err != nil || got {
		t.Errorf("no-subscriber feed = %v (err %v), want false (fail open)", got, err)
	}
}

// The summary is shared, so opting out must blank it per-user at serve time —
// in EVERY path that returns a summary to a user.
func TestSummarizeOptOut_BlanksSummaryPerUser(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	alice, bob, feedID, articleID := seedTwoSubscribers(t, s)
	setSummarize(t, s, bob, feedID, false)

	q := ListArticlesQuery{Limit: 10}

	// ListArticles — also covers the digest email, which goes through it.
	aliceList, err := s.ListArticles(ctx, alice, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(aliceList) != 1 || aliceList[0].Summary == "" {
		t.Fatalf("alice opted in but got summary %q", firstSummary(aliceList))
	}
	bobList, err := s.ListArticles(ctx, bob, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(bobList) != 1 {
		t.Fatalf("bob should still SEE the article (only the summary is hidden), got %d", len(bobList))
	}
	if bobList[0].Summary != "" || bobList[0].SummaryModel != "" {
		t.Errorf("bob opted out but got summary=%q model=%q", bobList[0].Summary, bobList[0].SummaryModel)
	}

	// GetArticleForUser — the reader pane.
	aliceOne, err := s.GetArticleForUser(ctx, alice, articleID)
	if err != nil {
		t.Fatal(err)
	}
	if aliceOne.Summary == "" {
		t.Error("alice lost her summary in GetArticleForUser")
	}
	bobOne, err := s.GetArticleForUser(ctx, bob, articleID)
	if err != nil {
		t.Fatal(err)
	}
	if bobOne.Summary != "" || bobOne.SummaryModel != "" {
		t.Errorf("GetArticleForUser leaked the summary to bob: %q / %q", bobOne.Summary, bobOne.SummaryModel)
	}

	// Search — the path most easily forgotten.
	aliceHits, err := s.Search(ctx, alice, "Shared", 10, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(aliceHits) == 0 || aliceHits[0].Summary == "" {
		t.Error("alice lost her summary in Search")
	}
	bobHits, err := s.Search(ctx, bob, "Shared", 10, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(bobHits) == 0 {
		t.Fatal("bob should still match the article in search")
	}
	if bobHits[0].Summary != "" || bobHits[0].SummaryModel != "" {
		t.Errorf("Search leaked the summary to bob: %q / %q", bobHits[0].Summary, bobHits[0].SummaryModel)
	}

	// The stored row is untouched — blanking is a per-user view, not a delete.
	raw, err := s.GetArticle(ctx, articleID)
	if err != nil {
		t.Fatal(err)
	}
	if raw.Summary == "" {
		t.Error("the shared article row lost its summary — blanking must not mutate storage")
	}
}

// Admin "Resummarize all" must not wipe the 'excluded' marker: doing so would
// re-queue every deliberately-skipped article AND briefly hide them behind the
// summary gate while summary_model sat NULL.
func TestClearAllSummaries_PreservesExcluded(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	_, feedID := seedUserAndFeed(t, s, "alice")

	real := mkArticle(feedID, "real", "Real", "h-real", s.Now().Unix())
	real.Summary, real.SummaryModel = "text", "test-model"
	realStored, _, err := s.UpsertArticle(ctx, real)
	if err != nil {
		t.Fatal(err)
	}
	excluded := mkArticle(feedID, "excl", "Excluded", "h-excl", s.Now().Unix())
	exclStored, _, err := s.UpsertArticle(ctx, excluded)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSummary(ctx, exclStored.ID, "", "excluded"); err != nil {
		t.Fatal(err)
	}

	ids, err := s.ClearAllSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if id == exclStored.ID {
			t.Error("ClearAllSummaries returned the excluded article for re-enqueue")
		}
	}
	got, err := s.GetArticle(ctx, exclStored.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummaryModel != "excluded" {
		t.Errorf("excluded marker was cleared: summary_model = %q", got.SummaryModel)
	}
	// ...and the genuinely-summarized one WAS reset, so the guard isn't too broad.
	if r, _ := s.GetArticle(ctx, realStored.ID); r.SummaryModel != "" {
		t.Errorf("real summary not reset: %q", r.SummaryModel)
	}
}

// Turning summaries back on must recover the articles skipped while off.
func TestResetExcludedByFeed(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	_, feedID := seedUserAndFeed(t, s, "alice")

	excl := mkArticle(feedID, "e1", "Excluded", "h-e1", s.Now().Unix())
	exclStored, _, _ := s.UpsertArticle(ctx, excl)
	if err := s.UpdateSummary(ctx, exclStored.ID, "", "excluded"); err != nil {
		t.Fatal(err)
	}
	skipped := mkArticle(feedID, "s1", "Skipped", "h-s1", s.Now().Unix())
	skipStored, _, _ := s.UpsertArticle(ctx, skipped)
	if err := s.UpdateSummary(ctx, skipStored.ID, "", "skipped"); err != nil {
		t.Fatal(err)
	}

	ids, err := s.ResetExcludedByFeed(ctx, feedID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != exclStored.ID {
		t.Fatalf("returned ids = %v, want just the excluded article %d", ids, exclStored.ID)
	}
	if got, _ := s.GetArticle(ctx, exclStored.ID); got.SummaryModel != "" {
		t.Errorf("excluded article not reset: %q", got.SummaryModel)
	}
	// 'skipped' belongs to Resummarize, not to the opt-in path.
	if got, _ := s.GetArticle(ctx, skipStored.ID); got.SummaryModel != "skipped" {
		t.Errorf("ResetExcludedByFeed also reset a 'skipped' article: %q", got.SummaryModel)
	}
}

// The "Summarizing N articles" indicator must not count work the user will
// never see — the same reasoning the query already applies to muted feeds.
func TestPendingSummary_ExcludesOptedOutFeeds(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	userID, feedID := seedUserAndFeed(t, s, "alice")
	if _, _, err := s.UpsertArticle(ctx,
		mkArticle(feedID, "p1", "Pending", "h-p1", s.Now().Unix())); err != nil {
		t.Fatal(err)
	}

	counts, err := s.CountSmartViews(ctx, userID, 6*time.Hour, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if counts.PendingSummary != 1 {
		t.Fatalf("precondition: pending = %d, want 1", counts.PendingSummary)
	}

	setSummarize(t, s, userID, feedID, false)
	counts, err = s.CountSmartViews(ctx, userID, 6*time.Hour, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if counts.PendingSummary != 0 {
		t.Errorf("pending = %d after opting out, want 0", counts.PendingSummary)
	}
}

// Subscribing defaults to opted IN, and the flag survives the store round-trip
// through all three read paths — the positional SELECT/scan pairs are easy to
// update in one place and forget in another.
func TestSubscriptionSummarize_RoundTrips(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	userID, feedID := seedUserAndFeed(t, s, "alice")

	sub, err := s.GetSubscription(ctx, userID, feedID)
	if err != nil {
		t.Fatal(err)
	}
	if !sub.Summarize {
		t.Fatal("new subscriptions must default to summarize=true")
	}

	setSummarize(t, s, userID, feedID, false)

	if got, _ := s.GetSubscription(ctx, userID, feedID); got.Summarize {
		t.Error("GetSubscription still reports summarize=true")
	}
	if got, err := s.GetSubscriptionByID(ctx, userID, sub.ID); err != nil || got.Summarize {
		t.Errorf("GetSubscriptionByID = %v (err %v), want summarize=false", got.Summarize, err)
	}
	feeds, err := s.ListFeedsForUser(ctx, userID, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 1 || feeds[0].Summarize {
		t.Error("ListFeedsForUser still reports summarize=true — the SPA renders from this")
	}
}

// The 'excluded' marker has to SATISFY the summary gate, not merely be
// non-empty. If it didn't, opting a feed out would hide its articles for the
// whole grace window — turning "don't summarize this" into "delay this", which
// is the exact behaviour issue #162 existed to remove.
func TestExcluded_IsVisibleImmediatelyUnderTheGate(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	userID, feedID := seedUserAndFeed(t, s, "alice")
	now := s.Now().Unix()

	// fetched_at = NOW, so the grace window has NOT elapsed for either article.
	// That isolates the marker: anything visible here is visible because of
	// summary_model, not because it timed out of the gate.
	excl := mkArticle(feedID, "e1", "Excluded", "h-e1", now)
	excl.FetchedAt = now
	exclStored, _, err := s.UpsertArticle(ctx, excl)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSummary(ctx, exclStored.ID, "", "excluded"); err != nil {
		t.Fatal(err)
	}
	pending := mkArticle(feedID, "p1", "Pending", "h-p1", now)
	pending.FetchedAt = now
	pendStored, _, err := s.UpsertArticle(ctx, pending)
	if err != nil {
		t.Fatal(err)
	}

	q := ListArticlesQuery{
		Limit:              10,
		OnlySummarized:     true,
		SummaryGraceBefore: now - 120,
	}
	list, err := s.ListArticles(ctx, userID, q)
	if err != nil {
		t.Fatal(err)
	}
	var sawExcluded, sawPending bool
	for _, a := range list {
		switch a.ID {
		case exclStored.ID:
			sawExcluded = true
		case pendStored.ID:
			sawPending = true
		}
	}
	// Control: the gate really is engaged. Without this the assertion below
	// would also pass if OnlySummarized were being ignored entirely.
	if sawPending {
		t.Fatal("precondition: an unsummarized article inside the grace window should be hidden — the gate isn't engaged, so this test proves nothing")
	}
	if !sawExcluded {
		t.Error("an opted-out article is hidden behind the summary gate — opting out must not become opting into a delay")
	}

	// The badge has to agree with the column, same as every other gate site.
	counts, err := s.CountSmartViews(ctx, userID, 6*time.Hour, 0, true, now-120)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Unread != 1 {
		t.Errorf("unread badge = %d, want 1 (the excluded article, not the pending one)", counts.Unread)
	}
	feeds, err := s.ListFeedsForUser(ctx, userID, 0, true, now-120)
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 1 || feeds[0].Unread != 1 {
		t.Errorf("per-feed unread = %+v, want 1 — ListFeedsForUser hand-rolls its own gate and drifts easily", feeds)
	}
}

// The 'shared' view rebuilds its FROM clause from scratch, swapping the
// subscriptions join for a shares join — so it was the one path where the
// per-user summary projection had no `s` alias to read. It is joined LEFT now,
// which has to mean both of these at once.
func TestSharedView_SummaryProjection(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	alice, bob, feedID, articleID := seedTwoSubscribers(t, s)

	// carol receives shares but does NOT subscribe to the feed.
	carol, err := s.CreateUser(ctx, models.User{Username: "carol", PasswordHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	for _, to := range []int64{bob, carol.ID} {
		if _, err := s.CreateShare(ctx, models.Share{
			ArticleID: articleID, FromUser: alice, ToUser: to,
		}); err != nil {
			t.Fatal(err)
		}
	}
	setSummarize(t, s, bob, feedID, false)

	q := ListArticlesQuery{Limit: 10, View: "shared"}

	// A non-subscriber has nothing to opt out OF. Their NULL must fall through
	// to the summary being shown — the behaviour before the join existed.
	carolList, err := s.ListArticles(ctx, carol.ID, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(carolList) != 1 {
		t.Fatalf("carol's shared view = %d articles, want 1", len(carolList))
	}
	if carolList[0].Summary == "" {
		t.Error("a shared article lost its summary for a non-subscriber — the LEFT JOIN is behaving like an INNER one")
	}

	// A subscriber who opted out stays opted out here too; a share is not a
	// side door around it.
	bobList, err := s.ListArticles(ctx, bob, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(bobList) != 1 {
		t.Fatalf("bob's shared view = %d articles, want 1 (opting out hides the summary, never the article)", len(bobList))
	}
	if bobList[0].Summary != "" || bobList[0].SummaryModel != "" {
		t.Errorf("the shared view leaked the summary to an opted-out subscriber: %q / %q",
			bobList[0].Summary, bobList[0].SummaryModel)
	}
}

// Opting out changes the summary TEXT, never which rows exist. The badge==column
// invariant every other gate has to hold must therefore be untouched by it.
func TestSummarizeOptOut_DoesNotMoveAnyBadge(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	userID, feedID := seedUserAndFeed(t, s, "alice")
	for _, g := range []string{"a1", "a2", "a3"} {
		art := mkArticle(feedID, g, "Story "+g, "h-"+g, s.Now().Add(-time.Hour).Unix())
		art.Summary, art.SummaryModel = "sum "+g, "test-model"
		if _, _, err := s.UpsertArticle(ctx, art); err != nil {
			t.Fatal(err)
		}
	}

	before, err := s.CountSmartViews(ctx, userID, 6*time.Hour, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	setSummarize(t, s, userID, feedID, false)
	after, err := s.CountSmartViews(ctx, userID, 6*time.Hour, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if before.Unread != after.Unread || before.Fresh != after.Fresh {
		t.Errorf("opting out moved a badge: unread %d->%d, fresh %d->%d",
			before.Unread, after.Unread, before.Fresh, after.Fresh)
	}
	list, err := s.ListArticles(ctx, userID, ListArticlesQuery{Limit: 10, View: "unread"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != after.Unread {
		t.Errorf("badge %d != column %d after opting out", after.Unread, len(list))
	}
	for _, a := range list {
		if a.Summary != "" {
			t.Errorf("article %d kept its summary after opting out: %q", a.ID, a.Summary)
		}
	}
}

func firstSummary(list []models.ArticleView) string {
	if len(list) == 0 {
		return "<empty list>"
	}
	return list[0].Summary
}
