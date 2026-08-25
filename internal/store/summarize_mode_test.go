package store

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/brandonhon/ember/internal/models"
)

// seedFeedWithTwoSubscribers creates a shared feed with two subscribers,
// alice and bob, both defaulting to summarize_mode = ” (inherit).
func seedFeedWithTwoSubscribers(t *testing.T, s *Store) (feed models.Feed, alice, bob int64) {
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
	f, err := s.UpsertFeed(ctx, models.Feed{URL: "https://mode.test/feed", Title: "Mode"})
	if err != nil {
		t.Fatal(err)
	}
	for _, uid := range []int64{a.ID, b.ID} {
		if _, err := s.Subscribe(ctx, models.Subscription{UserID: uid, FeedID: f.ID}); err != nil {
			t.Fatal(err)
		}
	}
	return f, a.ID, b.ID
}

// setSubscriptionMode writes a subscriber's summarize_mode directly. There is
// deliberately no store setter for this in Task 11's scope — the API path
// that will expose it belongs to a later task — so the test reaches the
// column the same way UpdateSubscription's other fields are exercised
// (through raw SQL) rather than inventing a method contract nothing else uses.
func setSubscriptionMode(t *testing.T, s *Store, userID, feedID int64, mode string) {
	t.Helper()
	res, err := s.DB.ExecContext(context.Background(),
		`UPDATE subscriptions SET summarize_mode = ? WHERE user_id = ? AND feed_id = ?`,
		mode, userID, feedID)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("setSubscriptionMode: %d rows affected, want 1", n)
	}
}

// insertTestArticle creates a fresh user, feed, subscription, and one
// article, returning the stored article.
func insertTestArticle(t *testing.T, s *Store) models.Article {
	t.Helper()
	ctx := context.Background()
	_, feedID := seedUserAndFeed(t, s, "art-"+randSuffix(t))
	stored, _, err := s.UpsertArticle(ctx, mkArticle(feedID, "g-"+randSuffix(t), "T", "h-"+randSuffix(t), 1000))
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

// randSuffix keeps insertTestArticle's synthetic usernames/guids/hashes
// unique across calls within one test — seedUserAndFeed's username becomes a
// feed URL, and a repeated URL/guid would silently dedup instead of creating
// a second article.
var randSuffixCounter int

func randSuffix(t *testing.T) string {
	t.Helper()
	randSuffixCounter++
	return strconv.Itoa(randSuffixCounter)
}

func TestFeedSummarizeMode_AnySubscriberWantingAllWins(t *testing.T) {
	st := NewTest(t)
	ctx := context.Background()
	feed, alice, bob := seedFeedWithTwoSubscribers(t, st)

	// Both inherit, global is on_demand → the feed is on_demand.
	if got, _ := st.FeedSummarizeMode(ctx, feed.ID, ModeOnDemand); got != ModeOnDemand {
		t.Fatalf("got %q, want on_demand", got)
	}
	// Alice pins her subscription to 'all' → the feed is 'all', because the
	// summary lives on the shared article row and Bob's frugality must not
	// take away the summary Alice asked for.
	setSubscriptionMode(t, st, alice, feed.ID, ModeAll)
	if got, _ := st.FeedSummarizeMode(ctx, feed.ID, ModeOnDemand); got != ModeAll {
		t.Fatalf("got %q, want all", got)
	}
	// Bob explicitly picking on_demand does not undo Alice's choice.
	setSubscriptionMode(t, st, bob, feed.ID, ModeOnDemand)
	if got, _ := st.FeedSummarizeMode(ctx, feed.ID, ModeOnDemand); got != ModeAll {
		t.Fatalf("got %q, want all", got)
	}
}

// A subscriber who INHERITS while the global mode is 'all' also counts as
// wanting 'all' — inherit is not a third opinion, it's "whatever the server
// says", and the server says 'all' here.
func TestFeedSummarizeMode_InheritCountsAsAllWhenGlobalIsAll(t *testing.T) {
	st := NewTest(t)
	ctx := context.Background()
	feed, _, _ := seedFeedWithTwoSubscribers(t, st)

	if got, err := st.FeedSummarizeMode(ctx, feed.ID, ModeAll); err != nil || got != ModeAll {
		t.Fatalf("got %q (err %v), want all", got, err)
	}
}

// A feed with no subscribers must fail open to globalMode, matching
// FeedSummariesSuppressed's rationale: handleAddFeed's UpsertFeed -> Subscribe
// -> RefreshFeed sequence leaves a window where the poller can ingest before
// the first subscription exists.
func TestFeedSummarizeMode_NoSubscribersFailsOpen(t *testing.T) {
	st := NewTest(t)
	ctx := context.Background()
	f, err := st.UpsertFeed(ctx, models.Feed{URL: "https://orphan.test/feed", Title: "Orphan"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := st.FeedSummarizeMode(ctx, f.ID, ModeOnDemand); err != nil || got != ModeOnDemand {
		t.Errorf("got %q (err %v), want on_demand (fail open)", got, err)
	}
	if got, err := st.FeedSummarizeMode(ctx, f.ID, ModeAll); err != nil || got != ModeAll {
		t.Errorf("got %q (err %v), want all (fail open)", got, err)
	}
}

func TestRequestSummary_OnlyClearsDeferred(t *testing.T) {
	st := NewTest(t)
	ctx := context.Background()

	deferred := insertTestArticle(t, st)
	skipped := insertTestArticle(t, st)
	excluded := insertTestArticle(t, st)
	if err := st.UpdateSummary(ctx, deferred.ID, "", "deferred"); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateSummary(ctx, skipped.ID, "", "skipped"); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateSummary(ctx, excluded.ID, "", "excluded"); err != nil {
		t.Fatal(err)
	}

	changed, err := st.RequestSummary(ctx, deferred.ID)
	if err != nil || !changed {
		t.Fatalf("changed = %v err = %v, want true/nil", changed, err)
	}
	got, err := st.GetArticle(ctx, deferred.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummaryModel != "" {
		t.Errorf("deferred marker not cleared: %q", got.SummaryModel)
	}

	// Starring a failed article must not silently retry it — Resummarize is the
	// action for that, and a star should never cost an unexpected inference run.
	changed, err = st.RequestSummary(ctx, skipped.ID)
	if err != nil || changed {
		t.Fatalf("changed = %v err = %v, want false/nil for a 'skipped' article", changed, err)
	}
	if got, _ := st.GetArticle(ctx, skipped.ID); got.SummaryModel != "skipped" {
		t.Errorf("'skipped' marker was disturbed: %q", got.SummaryModel)
	}

	// A feed opt-out must not be quietly reversed by a star either.
	changed, err = st.RequestSummary(ctx, excluded.ID)
	if err != nil || changed {
		t.Fatalf("changed = %v err = %v, want false/nil for an 'excluded' article", changed, err)
	}
	if got, _ := st.GetArticle(ctx, excluded.ID); got.SummaryModel != "excluded" {
		t.Errorf("'excluded' marker was disturbed: %q", got.SummaryModel)
	}
}

// ArticleSummaryRequested is the gate Task 12 actually consults — not whether
// the 'deferred' marker was cleared, but whether a reader has ACTUALLY starred,
// saved, or pinned the article. This makes the gate robust to the in-memory
// summary queue dropping ids under load: a dropped id just means the gate
// re-derives the same answer from the marks on its next pass.
func TestArticleSummaryRequested(t *testing.T) {
	st := NewTest(t)
	ctx := context.Background()
	userID, feedID := seedUserAndFeed(t, st, "carol")

	plain, _, err := st.UpsertArticle(ctx, mkArticle(feedID, "plain", "Plain", "h-plain", 1000))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := st.ArticleSummaryRequested(ctx, plain.ID); err != nil || got {
		t.Fatalf("untouched article requested = %v (err %v), want false", got, err)
	}

	// Starred.
	starred, _, err := st.UpsertArticle(ctx, mkArticle(feedID, "starred", "Starred", "h-starred", 1000))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetStarred(ctx, userID, starred.ID, true); err != nil {
		t.Fatal(err)
	}
	if got, err := st.ArticleSummaryRequested(ctx, starred.ID); err != nil || !got {
		t.Errorf("starred article requested = %v (err %v), want true", got, err)
	}

	// Saved for later.
	later, _, err := st.UpsertArticle(ctx, mkArticle(feedID, "later", "Later", "h-later", 1000))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetLater(ctx, userID, later.ID, true); err != nil {
		t.Fatal(err)
	}
	if got, err := st.ArticleSummaryRequested(ctx, later.ID); err != nil || !got {
		t.Errorf("read-later article requested = %v (err %v), want true", got, err)
	}

	// Pinned to a board that summarizes (the default).
	pinned, _, err := st.UpsertArticle(ctx, mkArticle(feedID, "pinned", "Pinned", "h-pinned", 1000))
	if err != nil {
		t.Fatal(err)
	}
	board, err := st.CreateBoard(ctx, models.Board{UserID: userID, Name: "Reading"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddArticleToBoard(ctx, userID, board.ID, pinned.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := st.ArticleSummaryRequested(ctx, pinned.ID); err != nil || !got {
		t.Errorf("pinned-to-summarizing-board article requested = %v (err %v), want true", got, err)
	}

	// Pinned to a board with summarize = 0 must NOT request a summary — a
	// board a user files things in rather than reads from must not trigger
	// inference just because an article landed there.
	filed, _, err := st.UpsertArticle(ctx, mkArticle(feedID, "filed", "Filed", "h-filed", 1000))
	if err != nil {
		t.Fatal(err)
	}
	archive, err := st.CreateBoard(ctx, models.Board{UserID: userID, Name: "Archive"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateBoard(ctx, userID, archive.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := st.AddArticleToBoard(ctx, userID, archive.ID, filed.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := st.ArticleSummaryRequested(ctx, filed.ID); err != nil || got {
		t.Errorf("pinned-to-non-summarizing-board article requested = %v (err %v), want false", got, err)
	}
}

// UpdateBoard and BoardSummarizes are per-user resources scoped by user_id,
// matching GetBoard: a foreign board id must return ErrNotFound, never a
// silent success or another user's data.
func TestUpdateBoardAndBoardSummarizes_RejectForeignBoard(t *testing.T) {
	st := NewTest(t)
	ctx := context.Background()
	alice, err := st.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := st.CreateUser(ctx, models.User{Username: "bob", PasswordHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	board, err := st.CreateBoard(ctx, models.Board{UserID: alice.ID, Name: "Reading"})
	if err != nil {
		t.Fatal(err)
	}
	if !board.Summarize {
		t.Fatal("a newly created board must default to summarize = true")
	}

	// Bob cannot flip alice's board.
	if err := st.UpdateBoard(ctx, bob.ID, board.ID, false); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-user UpdateBoard = %v, want ErrNotFound", err)
	}
	if got, err := st.BoardSummarizes(ctx, alice.ID, board.ID); err != nil || !got {
		t.Errorf("alice's board was mutated by bob's rejected call: got=%v err=%v", got, err)
	}
	// Bob cannot read it either.
	if _, err := st.BoardSummarizes(ctx, bob.ID, board.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-user BoardSummarizes = %v, want ErrNotFound", err)
	}

	// Alice can flip her own board.
	if err := st.UpdateBoard(ctx, alice.ID, board.ID, false); err != nil {
		t.Fatal(err)
	}
	if got, err := st.BoardSummarizes(ctx, alice.ID, board.ID); err != nil || got {
		t.Errorf("BoardSummarizes after UpdateBoard(false) = %v (err %v), want false", got, err)
	}

	// A board id that doesn't exist at all behaves the same way.
	if err := st.UpdateBoard(ctx, alice.ID, board.ID+999, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateBoard on nonexistent id = %v, want ErrNotFound", err)
	}
	if _, err := st.BoardSummarizes(ctx, alice.ID, board.ID+999); !errors.Is(err, ErrNotFound) {
		t.Errorf("BoardSummarizes on nonexistent id = %v, want ErrNotFound", err)
	}
}

func TestResolvePutSummarizeMode(t *testing.T) {
	st := NewTest(t)
	ctx := context.Background()

	// Unset → fallback, whichever valid value it is.
	if got := st.ResolveSummarizeMode(ctx, ModeOnDemand); got != ModeOnDemand {
		t.Errorf("unset, fallback=on_demand: got %q", got)
	}
	// An invalid fallback (e.g. the zero value ModeInherit) is not a valid
	// server-wide mode, so it must not be trusted — corrupt input falls back
	// to ModeAll, matching every existing install's behaviour.
	if got := st.ResolveSummarizeMode(ctx, ModeInherit); got != ModeAll {
		t.Errorf("unset, invalid fallback: got %q, want all", got)
	}

	if err := st.PutSummarizeMode(ctx, ModeOnDemand); err != nil {
		t.Fatal(err)
	}
	if got := st.ResolveSummarizeMode(ctx, ModeAll); got != ModeOnDemand {
		t.Errorf("stored value should win over fallback: got %q", got)
	}

	if err := st.PutSummarizeMode(ctx, "bogus"); err == nil {
		t.Error("PutSummarizeMode accepted an invalid mode")
	}
	// The rejected write must not have clobbered the previously stored value.
	if got := st.ResolveSummarizeMode(ctx, ModeAll); got != ModeOnDemand {
		t.Errorf("rejected write disturbed the stored mode: got %q", got)
	}
}
