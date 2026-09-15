package poller

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
	"github.com/brandonhon/ember/internal/summarize"
)

// countingSummarizer records how many times a model was actually asked to work.
type countingSummarizer struct{ calls atomic.Int64 }

func (c *countingSummarizer) Summarize(_ context.Context, _, title string) (summarize.Result, string, error) {
	c.calls.Add(1)
	return summarize.Result{Paragraph: "s: " + title}, "counting", nil
}

// The runtime switch is enforced at the consumer, which is the only place that
// sees every enqueue path (poller ingest, the email inbox, ClearAllSummaries,
// ResetSummariesByFeed, the manual re-enqueue behind Resummarize). With the
// switch off, summarizeOne must stamp a terminal marker instead of calling the
// model: leaving the row NULL is precisely what made issue #198's queue refill
// forever with articles nobody would ever finalize, invisible behind the
// summary gate the whole time.
func TestSummarizeOne_StampsDisabledWhenSwitchedOff(t *testing.T) {
	ctx := context.Background()
	st := store.NewTest(t)
	sum := &countingSummarizer{}
	p := newSummaryTestPoller(t, st, sum)
	art := insertSummaryTestArticle(t, st)

	if err := st.PutSummariesEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	p.summarizeOne(ctx, art.ID)

	if n := sum.calls.Load(); n != 0 {
		t.Errorf("summarizer was called %d times with summaries switched off", n)
	}
	got, err := st.GetArticle(ctx, art.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummaryModel != "disabled" {
		t.Errorf("summary_model = %q, want \"disabled\" — a NULL marker leaves the article hidden and re-queued forever", got.SummaryModel)
	}

	// Switched back on, the same consumer does the real work.
	if err := st.PutSummariesEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateSummary(ctx, art.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	p.summarizeOne(ctx, art.ID)
	if n := sum.calls.Load(); n != 1 {
		t.Errorf("summarizer called %d times after re-enabling, want 1", n)
	}
	if got, err := st.GetArticle(ctx, art.ID); err != nil {
		t.Fatal(err)
	} else if got.SummaryModel != "counting" {
		t.Errorf("summary_model = %q, want the model name", got.SummaryModel)
	}
}

// The per-tick backfill has to stop too. Without this it keeps handing the
// worker articles it will only stamp, tick after tick, for as long as the
// switch stays off.
func TestEnqueuePendingSummaries_NoOpWhenSwitchedOff(t *testing.T) {
	ctx := context.Background()
	st := store.NewTest(t)
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := New(st, &fakeFetcher{notModified: true}, &countingSummarizer{}, Config{
		Tick: time.Hour, Concurrency: 1, SummaryQueue: 8,
		SummariesEnabledFallback: true,
	}, lg)

	f := seedFeed(t, st)
	if _, _, err := st.UpsertArticle(ctx, models.Article{
		FeedID: f.ID, GUID: "pending", Title: "pending",
		ContentHash: "h-pending", PublishedAt: time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}

	p.enqueuePendingSummaries(ctx)
	if n := len(p.summaryCh); n != 1 {
		t.Fatalf("precondition: queued %d with summaries on, want 1", n)
	}
	<-p.summaryCh

	if err := st.PutSummariesEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	p.enqueuePendingSummaries(ctx)
	if n := len(p.summaryCh); n != 0 {
		t.Errorf("queued %d articles with summaries switched off; the backfill must stand down", n)
	}
}

// Standing down is not enough — while the switch is off, this is the ONLY path
// that finalizes a pending row, and no crash is needed to depend on it. The
// realistic case: an admin turns summaries off while ids are still sitting in
// the 256-entry summaryCh. Run returns on ctx.Done() without draining that
// buffer, so those articles keep summary_model NULL; on the next start both
// backfills see the switch off and the Summarizer != nil startup heal doesn't
// apply. Without the drain here they stay invisible behind the summary gate,
// pinned in "Summarizing N articles", across every restart — issue #198
// verbatim, "restarting the container did not clear it" included.
func TestEnqueuePendingSummaries_DrainsBacklogWhenSwitchedOff(t *testing.T) {
	ctx := context.Background()
	st := store.NewTest(t)
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	sum := &countingSummarizer{}
	p := New(st, &fakeFetcher{notModified: true}, sum, Config{
		Tick: time.Hour, Concurrency: 1, SummaryQueue: 8,
		SummariesEnabledFallback: true,
	}, lg)

	f := seedFeed(t, st)
	user, err := st.CreateUser(ctx, models.User{Username: "reader", PasswordHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Subscribe(ctx, models.Subscription{UserID: user.ID, FeedID: f.ID}); err != nil {
		t.Fatal(err)
	}
	// Stand in for ids the previous process dropped on the floor at shutdown:
	// articles that reached the DB but never reached a model.
	var stranded []int64
	for i := 0; i < 3; i++ {
		guid := fmt.Sprintf("stranded-%d", i)
		a, _, err := st.UpsertArticle(ctx, models.Article{
			FeedID: f.ID, GUID: guid, Title: guid,
			ContentHash: "h-" + guid, PublishedAt: time.Now().Unix(),
		})
		if err != nil {
			t.Fatal(err)
		}
		stranded = append(stranded, a.ID)
	}
	counts, err := st.CountSmartViews(ctx, user.ID, time.Hour, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if counts.PendingSummary != 3 {
		t.Fatalf("precondition: pending_summary = %d, want 3", counts.PendingSummary)
	}

	// The switch was flipped off — by the settings handler, or seeded off at
	// boot — and this process never saw the handler's drain.
	if err := st.PutSummariesEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	p.enqueuePendingSummaries(ctx)

	if n := sum.calls.Load(); n != 0 {
		t.Errorf("summarizer was called %d times draining the backlog; nothing may be summarized while off", n)
	}
	for _, id := range stranded {
		got, err := st.GetArticle(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.SummaryModel != "disabled" {
			t.Errorf("article %d: summary_model = %q, want \"disabled\"", id, got.SummaryModel)
		}
	}
	counts, err = st.CountSmartViews(ctx, user.ID, time.Hour, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if counts.PendingSummary != 0 {
		t.Errorf("pending_summary = %d, want 0 — the backlog never converges and a restart won't clear it", counts.PendingSummary)
	}
	if n := len(p.summaryCh); n != 0 {
		t.Errorf("queued %d articles while draining; the backfill must not refill", n)
	}
}
