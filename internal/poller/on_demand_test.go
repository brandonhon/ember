package poller

import (
	"context"
	"testing"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// setSubscriptionMode writes a subscriber's summarize_mode directly. There is
// no store setter for this yet — the API path that exposes it belongs to a
// later task — so the test reaches the column the same way Task 11's own
// tests do: raw SQL against the same subscriptions row UpdateSubscription's
// other fields are exercised through.
func setSubscriptionMode(t *testing.T, st *store.Store, userID, feedID int64, mode string) {
	t.Helper()
	res, err := st.DB.ExecContext(context.Background(),
		`UPDATE subscriptions SET summarize_mode = ? WHERE user_id = ? AND feed_id = ?`,
		mode, userID, feedID)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("setSubscriptionMode: %d rows affected, want 1", n)
	}
}

// pendingIDs reports whether id is among the articles ListUnsummarizedIDs
// would hand the worker next — i.e. whether it's still counted as pending.
func isPending(t *testing.T, st *store.Store, id int64) bool {
	t.Helper()
	ids, err := st.ListUnsummarizedIDs(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range ids {
		if got == id {
			return true
		}
	}
	return false
}

// starArticle creates a fresh subscribed user and stars the article on their
// behalf — the cheapest way to make ArticleSummaryRequested true for a given
// article id.
func starArticle(t *testing.T, st *store.Store, art models.Article) {
	t.Helper()
	ctx := context.Background()
	user, err := st.CreateUser(ctx, models.User{Username: "reader-" + art.GUID, PasswordHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	// SetStarred only writes state for articles the user is subscribed to.
	if _, err := st.Subscribe(ctx, models.Subscription{UserID: user.ID, FeedID: art.FeedID}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetStarred(ctx, user.ID, art.ID, true); err != nil {
		t.Fatal(err)
	}
}

// Ruling P2: the on-demand gate must key off whether the article is actually
// MARKED (starred, saved for later, or pinned to a summarize=1 board), not
// off whether the 'deferred' marker has been cleared. A marker-only gate is
// unsatisfiable — summarizeOne would re-stamp 'deferred' on every re-entry
// because the mode is still on-demand — so this test drives the real
// production path: RequestSummary only ever runs after the mark already
// exists (see the HTTP handlers), and it's the mark that lets the article
// through.
func TestSummarizeOne_OnDemandDefersUnmarkedArticle(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()
	if err := st.PutSummarizeMode(ctx, store.ModeOnDemand); err != nil {
		t.Fatal(err)
	}
	sum := &countingSummarizer{}
	p := newSummaryTestPoller(t, st, sum)
	art := insertSummaryTestArticle(t, st)

	p.summarizeOne(ctx, art.ID)

	if n := sum.calls.Load(); n != 0 {
		t.Fatalf("summarizer called %d times; on-demand must not spend inference at ingest", n)
	}
	got, err := st.GetArticle(ctx, art.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummaryModel != "deferred" {
		t.Fatalf("summary_model = %q, want %q", got.SummaryModel, "deferred")
	}
	// A deferred article is visible and out of the pending count — the whole
	// point is that the queue stays short enough that a summary the reader
	// did ask for is ready quickly.
	if isPending(t, st, art.ID) {
		t.Fatal("deferred article is still counted pending — it will be re-queued every tick")
	}
}

// THE proof case for Ruling P2. This is not "an already-marked article gets
// summarized" (that's a weaker claim any gate design satisfies) — it is the
// SAME article, across two summarizeOne calls: first deferred because
// unmarked, then genuinely summarized once marked. The brief's original
// design (gate re-reads the 'deferred' marker instead of the marks) is
// unimplementable against exactly this sequence: RequestSummary clears the
// marker between the two calls, and a marker-keyed gate would just re-stamp
// 'deferred' on the second summarizeOne, because the mode is still
// on-demand. Keying off ArticleSummaryRequested instead means the second
// call sees a real "yes" and lets the article through.
func TestSummarizeOne_OnDemandTransitionsFromDeferredToSummarized(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()
	if err := st.PutSummarizeMode(ctx, store.ModeOnDemand); err != nil {
		t.Fatal(err)
	}
	sum := &countingSummarizer{}
	p := newSummaryTestPoller(t, st, sum)
	art := insertSummaryTestArticle(t, st)

	// Step 1: unmarked, on-demand — defers.
	p.summarizeOne(ctx, art.ID)
	if n := sum.calls.Load(); n != 0 {
		t.Fatalf("summarizer called %d times before any mark; want 0", n)
	}
	got, err := st.GetArticle(ctx, art.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummaryModel != "deferred" {
		t.Fatalf("summary_model = %q after step 1, want %q", got.SummaryModel, "deferred")
	}

	// Step 2: mark the SAME article — the reader says they mean to read it.
	starArticle(t, st, art)
	requested, err := st.ArticleSummaryRequested(ctx, art.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !requested {
		t.Fatal("precondition: starring should make ArticleSummaryRequested true")
	}
	// RequestSummary is what the HTTP handlers actually call after a mark —
	// exercise it here too so the test covers the real end-to-end sequence,
	// not just the gate in isolation.
	if changed, err := st.RequestSummary(ctx, art.ID); err != nil {
		t.Fatal(err)
	} else if !changed {
		t.Fatal("precondition: RequestSummary should have cleared the 'deferred' marker")
	}

	// Step 3: re-entry on the SAME article id — must now be summarized, not
	// re-deferred.
	p.summarizeOne(ctx, art.ID)
	if n := sum.calls.Load(); n != 1 {
		t.Fatalf("summarizer called %d times after marking, want 1", n)
	}
	got, err = st.GetArticle(ctx, art.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummaryModel != "counting" {
		t.Fatalf("summary_model = %q after step 3, want the model name (not re-deferred)", got.SummaryModel)
	}
}

// A weaker companion to the transition test above: an article that was never
// deferred at all — marked before summarizeOne ever sees it — must also be
// summarized on the first call. Kept alongside the transition test rather
// than replacing it, since it covers a distinct (if less critical) path: a
// reader who stars an article before ingest's own first pass reaches it.
func TestSummarizeOne_OnDemandSummarizesAlreadyMarkedArticle(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()
	if err := st.PutSummarizeMode(ctx, store.ModeOnDemand); err != nil {
		t.Fatal(err)
	}
	sum := &countingSummarizer{}
	p := newSummaryTestPoller(t, st, sum)
	art := insertSummaryTestArticle(t, st)

	starArticle(t, st, art)
	requested, err := st.ArticleSummaryRequested(ctx, art.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !requested {
		t.Fatal("precondition: starring should make ArticleSummaryRequested true")
	}

	p.summarizeOne(ctx, art.ID)

	if n := sum.calls.Load(); n != 1 {
		t.Fatalf("summarizer called %d times for a starred article, want 1", n)
	}
	got, err := st.GetArticle(ctx, art.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummaryModel != "counting" {
		t.Fatalf("summary_model = %q, want the model name", got.SummaryModel)
	}
}

// Per-feed 'all' beats a global on_demand — the any-wins semantics of
// FeedSummarizeMode apply here exactly as they do to the opt-out check.
func TestSummarizeOne_PerFeedAllBeatsGlobalOnDemand(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()
	if err := st.PutSummarizeMode(ctx, store.ModeOnDemand); err != nil {
		t.Fatal(err)
	}
	sum := &countingSummarizer{}
	p := newSummaryTestPoller(t, st, sum)
	f := seedFeed(t, p.Store)
	user, err := st.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Subscribe(ctx, models.Subscription{UserID: user.ID, FeedID: f.ID}); err != nil {
		t.Fatal(err)
	}
	setSubscriptionMode(t, st, user.ID, f.ID, store.ModeAll)
	id := seedArticle(t, st, f.ID, "per-feed-all")

	p.summarizeOne(ctx, id)

	if n := sum.calls.Load(); n != 1 {
		t.Fatalf("summarizer called %d times; the subscriber pinned 'all' on this feed", n)
	}
	got, err := st.GetArticle(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummaryModel != "counting" {
		t.Fatalf("summary_model = %q, want the model name", got.SummaryModel)
	}
}

// The hard per-feed opt-out (issue #163) keeps winning over on-demand: an
// opted-out feed stamps 'excluded', never 'deferred'.
func TestSummarizeOne_FeedOptOutBeatsOnDemand(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()
	if err := st.PutSummarizeMode(ctx, store.ModeOnDemand); err != nil {
		t.Fatal(err)
	}
	sum := &countingSummarizer{}
	p := newSummaryTestPoller(t, st, sum)
	f := seedFeed(t, p.Store)
	subscribeTo(t, p.Store, "alice", f.ID, false)
	id := seedArticle(t, st, f.ID, "opted-out-on-demand")

	p.summarizeOne(ctx, id)

	if n := sum.calls.Load(); n != 0 {
		t.Fatalf("summarizer called %d times for an opted-out feed", n)
	}
	got, err := st.GetArticle(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummaryModel != "excluded" {
		t.Fatalf("summary_model = %q, want %q (opt-out must win over on-demand)", got.SummaryModel, "excluded")
	}
}
