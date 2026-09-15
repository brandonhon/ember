package poller

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/store"
	"github.com/brandonhon/ember/internal/summarize"
)

// Once the API hands the poller a Switcher, p.Summarizer is never nil in
// production, so the nil-Summarizer branches can no longer detect "no backend".
// The state this test protects is real and easy to reach: an admin picks Claude
// in Settings and has not yet pasted the API key. Without the readiness hook
// every article the poller ingests in that window is handed to a backend that
// errors, and summarizeOne's failure path stamps 'skipped' — terminal, and only
// undone by a manual Resummarize of the whole database. 'disabled' is the
// recoverable marker: the Requeue action puts exactly those articles back.
func TestSummarizeOne_StampsDisabledWhenBackendUnconfigured(t *testing.T) {
	ctx := context.Background()
	st := store.NewTest(t)
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))

	// A Switcher with nothing behind it: exactly what BuildBackend returns for
	// a hosted backend that is missing its credential.
	backend := &summarize.Switcher{}
	p := New(st, &fakeFetcher{notModified: true}, backend, Config{
		Tick: time.Hour, Concurrency: 1,
		SummariesEnabledFallback: true,
		SummarizerReady:          backend.Configured,
	}, lg)
	art := insertSummaryTestArticle(t, st)

	p.summarizeOne(ctx, art.ID)

	got, err := st.GetArticle(ctx, art.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummaryModel != "disabled" {
		t.Errorf("summary_model = %q, want \"disabled\" — %q is terminal and strands the article until a full Resummarize",
			got.SummaryModel, got.SummaryModel)
	}

	// Finish configuring the backend and the same consumer does the real work,
	// with no marker left over to clean up beyond the requeue.
	backend.Set(&countingSummarizer{})
	if err := st.UpdateSummary(ctx, art.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	p.summarizeOne(ctx, art.ID)
	if got, err := st.GetArticle(ctx, art.ID); err != nil {
		t.Fatal(err)
	} else if got.SummaryModel != "counting" {
		t.Errorf("summary_model = %q, want the model name once the backend is configured", got.SummaryModel)
	}
}

// The per-tick backfill has to stand down for an unconfigured backend too, and
// finalize the backlog rather than merely stopping: otherwise the pending rows
// stay invisible behind the summary gate and pinned in "Summarizing N
// articles" for as long as the admin takes to find the API key.
func TestEnqueuePendingSummaries_StampsBacklogWhenBackendUnconfigured(t *testing.T) {
	ctx := context.Background()
	st := store.NewTest(t)
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))

	backend := &summarize.Switcher{}
	p := New(st, &fakeFetcher{notModified: true}, backend, Config{
		Tick: time.Hour, Concurrency: 1,
		SummariesEnabledFallback: true,
		SummarizerReady:          backend.Configured,
	}, lg)
	art := insertSummaryTestArticle(t, st)

	p.enqueuePendingSummaries(ctx)

	if n := len(p.summaryCh); n != 0 {
		t.Errorf("queued %d articles at an unconfigured backend, want 0", n)
	}
	got, err := st.GetArticle(ctx, art.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummaryModel != "disabled" {
		t.Errorf("summary_model = %q, want \"disabled\"", got.SummaryModel)
	}
}

// A nil hook means "always ready" so every existing caller — the tests here and
// anything wiring a concrete backend — keeps its old behaviour.
func TestSummariesEnabled_NilReadyHookMeansReady(t *testing.T) {
	st := store.NewTest(t)
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := New(st, &fakeFetcher{notModified: true}, &countingSummarizer{}, Config{
		Tick: time.Hour, Concurrency: 1,
		SummariesEnabledFallback: true,
	}, lg)
	if !p.summariesEnabled(context.Background()) {
		t.Error("a nil SummarizerReady must not turn summaries off")
	}
}
