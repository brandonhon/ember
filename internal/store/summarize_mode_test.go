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

// setSubscriptionMode sets a subscriber's summarize_mode through the same
// store call the API uses. It wrote the column with raw SQL while no setter
// existed; now that UpdateSubscription carries SummarizeMode, going through
// it means these resolution tests exercise the production write path rather
// than a shortcut that could silently diverge from it.
func setSubscriptionMode(t *testing.T, s *Store, userID, feedID int64, mode string) {
	t.Helper()
	ctx := context.Background()
	sub, err := s.GetSubscription(ctx, userID, feedID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSubscription(ctx, userID, sub.ID, UpdateSubscriptionPatch{SummarizeMode: &mode}); err != nil {
		t.Fatal(err)
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

// The per-subscription mode has to survive the real write path, not just the
// column: UpdateSubscription is what the API calls, and a patch field that
// silently no-ops would leave the sidebar's three-way choice inert. ModeInherit
// is checked explicitly because "" is a meaningful value here — clearing an
// override back to "follow the server" is a distinct outcome from never having
// set one.
func TestUpdateSubscription_PersistsSummarizeMode(t *testing.T) {
	st := NewTest(t)
	ctx := context.Background()
	userID, feedID := seedUserAndFeed(t, st, "modewriter")
	sub, err := st.GetSubscription(ctx, userID, feedID)
	if err != nil {
		t.Fatal(err)
	}
	if sub.SummarizeMode != ModeInherit {
		t.Fatalf("precondition: new subscriptions start at inherit, got %q", sub.SummarizeMode)
	}

	for _, mode := range []string{ModeOnDemand, ModeAll, ModeInherit} {
		m := mode
		if err := st.UpdateSubscription(ctx, userID, sub.ID, UpdateSubscriptionPatch{SummarizeMode: &m}); err != nil {
			t.Fatalf("set %q: %v", mode, err)
		}
		got, err := st.GetSubscriptionByID(ctx, userID, sub.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.SummarizeMode != mode {
			t.Errorf("summarize_mode = %q, want %q", got.SummarizeMode, mode)
		}
	}

	// A patch that omits the field leaves the stored mode alone — the pointer
	// is what distinguishes "set it to inherit" from "don't touch it".
	onDemand := ModeOnDemand
	if err := st.UpdateSubscription(ctx, userID, sub.ID, UpdateSubscriptionPatch{SummarizeMode: &onDemand}); err != nil {
		t.Fatal(err)
	}
	muted := true
	if err := st.UpdateSubscription(ctx, userID, sub.ID, UpdateSubscriptionPatch{Muted: &muted}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSubscriptionByID(ctx, userID, sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummarizeMode != ModeOnDemand {
		t.Errorf("an unrelated patch changed summarize_mode to %q", got.SummarizeMode)
	}

	// Cross-user scoping is the same as every other field's: another user's
	// subscription id is not reachable.
	otherID, otherFeed := seedUserAndFeed(t, st, "modereader")
	otherSub, err := st.GetSubscription(ctx, otherID, otherFeed)
	if err != nil {
		t.Fatal(err)
	}
	all := ModeAll
	if err := st.UpdateSubscription(ctx, userID, otherSub.ID, UpdateSubscriptionPatch{SummarizeMode: &all}); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-user UpdateSubscription = %v, want ErrNotFound", err)
	}
	if got, _ := st.GetSubscriptionByID(ctx, otherID, otherSub.ID); got.SummarizeMode != ModeInherit {
		t.Errorf("cross-user patch mutated the target: summarize_mode = %q", got.SummarizeMode)
	}
}

// Switching a feed to "summarize every article" has to reach back for the
// articles on-demand mode already deferred, or the new setting looks inert
// until the feed publishes something. 'excluded' (the per-feed opt-out) and
// 'skipped' (a real failure) are deliberately left alone: neither means
// "nobody has asked for this one yet".
func TestResetDeferredByFeed(t *testing.T) {
	st := NewTest(t)
	ctx := context.Background()
	_, feedID := seedUserAndFeed(t, st, "deferred")

	seed := func(guid, marker string) int64 {
		a, _, err := st.UpsertArticle(ctx, mkArticle(feedID, guid, guid, "h-"+guid, 1000))
		if err != nil {
			t.Fatal(err)
		}
		if marker != "" {
			if err := st.UpdateSummary(ctx, a.ID, "", marker); err != nil {
				t.Fatal(err)
			}
		}
		return a.ID
	}
	deferredID := seed("g-deferred", "deferred")
	excludedID := seed("g-excluded", "excluded")
	skippedID := seed("g-skipped", "skipped")

	ids, err := st.ResetDeferredByFeed(ctx, feedID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != deferredID {
		t.Fatalf("ResetDeferredByFeed = %v, want [%d]", ids, deferredID)
	}
	if got, _ := st.GetArticle(ctx, deferredID); got.SummaryModel != "" {
		t.Errorf("deferred marker survived: %q", got.SummaryModel)
	}
	if got, _ := st.GetArticle(ctx, excludedID); got.SummaryModel != "excluded" {
		t.Errorf("excluded article was disturbed: %q", got.SummaryModel)
	}
	if got, _ := st.GetArticle(ctx, skippedID); got.SummaryModel != "skipped" {
		t.Errorf("skipped article was disturbed: %q", got.SummaryModel)
	}

	// Nothing left to reset: no ids, no error.
	again, err := st.ResetDeferredByFeed(ctx, feedID)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Errorf("second call returned %v, want none", again)
	}
}

// The server-wide twin of TestResetDeferredByFeed. An admin switching the
// global mode back to "every article" is owed the same backfill a feed-level
// switch gets — and the same narrowness: only 'deferred' means "nobody has
// asked for this one yet", so only 'deferred' is answered by a change of mode.
func TestResetAllDeferred(t *testing.T) {
	st := NewTest(t)
	ctx := context.Background()
	_, feedA := seedUserAndFeed(t, st, "deferred-a")
	_, feedB := seedUserAndFeed(t, st, "deferred-b")

	seed := func(feedID int64, guid, marker string) int64 {
		a, _, err := st.UpsertArticle(ctx, mkArticle(feedID, guid, guid, "h-"+guid, 1000))
		if err != nil {
			t.Fatal(err)
		}
		if marker != "" {
			if err := st.UpdateSummary(ctx, a.ID, "sum-"+guid, marker); err != nil {
				t.Fatal(err)
			}
		}
		return a.ID
	}
	// Deferred articles in two different feeds: the reset is global, so both
	// come back — that is the whole difference from ResetDeferredByFeed.
	deferredA := seed(feedA, "g-def-a", "deferred")
	deferredB := seed(feedB, "g-def-b", "deferred")
	untouched := map[string]int64{
		"skipped":   seed(feedA, "g-skipped", "skipped"),
		"excluded":  seed(feedA, "g-excluded", "excluded"),
		"disabled":  seed(feedA, "g-disabled", "disabled"),
		"llama3:8b": seed(feedB, "g-done", "llama3:8b"),
	}
	pending := seed(feedB, "g-pending", "")

	ids, err := st.ResetAllDeferred(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if len(ids) != 2 || !got[deferredA] || !got[deferredB] {
		t.Fatalf("ResetAllDeferred = %v, want exactly [%d %d]", ids, deferredA, deferredB)
	}
	for _, id := range []int64{deferredA, deferredB} {
		if a, _ := st.GetArticle(ctx, id); a.SummaryModel != "" {
			t.Errorf("article %d: summary_model = %q, want cleared", id, a.SummaryModel)
		}
	}
	// Every other state is left exactly as it was — asserted marker by marker,
	// because a count would pass even if the reset had swapped one state for
	// another.
	for marker, id := range untouched {
		a, err := st.GetArticle(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if a.SummaryModel != marker {
			t.Errorf("article stamped %q became %q", marker, a.SummaryModel)
		}
		if marker == "llama3:8b" && a.Summary == "" {
			t.Error("a finished summary's text was blanked")
		}
	}
	if a, _ := st.GetArticle(ctx, pending); a.SummaryModel != "" {
		t.Errorf("a pending article was stamped %q", a.SummaryModel)
	}

	// Nothing left to reset: no ids, no error.
	again, err := st.ResetAllDeferred(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Errorf("second call returned %v, want none", again)
	}
}
