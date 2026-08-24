package poller

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
	"github.com/brandonhon/ember/internal/summarize"
)

// slowSummarizer blocks until ctx is done, then reports how long it waited.
type slowSummarizer struct{ waited chan time.Duration }

func (s slowSummarizer) Summarize(ctx context.Context, _, _ string) (summarize.Result, string, error) {
	start := time.Now()
	<-ctx.Done()
	s.waited <- time.Since(start)
	return summarize.Result{}, "slow", ctx.Err()
}

// newSummaryTestPoller builds a Poller wired to sum, with no fetcher work to
// do — these tests drive summarizeOne directly rather than through Run/Tick.
func newSummaryTestPoller(t *testing.T, st *store.Store, sum summarize.Summarizer) *Poller {
	t.Helper()
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(st, &fakeFetcher{notModified: true}, sum, Config{
		Tick: time.Hour, Concurrency: 1,
	}, lg)
}

// insertSummaryTestArticle seeds a feed and one article with no summary yet.
func insertSummaryTestArticle(t *testing.T, st *store.Store) models.Article {
	t.Helper()
	f := seedFeed(t, st)
	stored, _, err := st.UpsertArticle(context.Background(), models.Article{
		FeedID: f.ID, GUID: "g-timeout", Title: "T", URL: "https://example.test/a",
		ContentText: "body", ContentHash: "h-timeout", PublishedAt: time.Now().Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

func TestSummarizeOne_AppliesConfiguredTimeout(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()
	if err := st.PutSummaryTimeoutSeconds(ctx, 10); err != nil { // clamped floor
		t.Fatal(err)
	}

	sum := slowSummarizer{waited: make(chan time.Duration, 1)}
	p := newSummaryTestPoller(t, st, sum)
	p.Config.SummaryTimeoutSecondsFallback = 90 // must lose to the stored 10s

	art := insertSummaryTestArticle(t, st)

	done := make(chan struct{})
	go func() { p.summarizeOne(ctx, art.ID); close(done) }()

	select {
	case waited := <-sum.waited:
		if waited > 30*time.Second {
			t.Fatalf("summarize ran for %s; the 10s stored timeout was not applied", waited)
		}
	case <-time.After(40 * time.Second):
		t.Fatal("summarize was never cancelled — no deadline was applied")
	}
	<-done

	// A timed-out attempt is terminal: the article is stamped 'skipped', not
	// left pending, so it is visible and never re-queued.
	got, err := st.GetArticle(ctx, art.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummaryModel != "skipped" {
		t.Fatalf("summary_model = %q, want \"skipped\"", got.SummaryModel)
	}
}

// TestSummarizeOne_TimeoutDoesNotRegenerate reproduces issue #201 directly
// against the real Ollama client: previously the 90s deadline lived on
// http.Client.Timeout, so when it fired ctx.Err() was still nil and
// Ollama.Summarize's internal retry immediately re-issued an identical
// /api/generate request, making the backend generate the same article twice.
// With the deadline on ctx instead, the first attempt's ctx.Err() is non-nil
// once the poller's per-article timeout expires, so the retry loop's guard
// fires and no second request is ever sent.
func TestSummarizeOne_TimeoutDoesNotRegenerate(t *testing.T) {
	var hits atomic.Int64
	// release, not r.Context().Done(), gates the handler: a POST with a body
	// whose client-side context expires does not reliably propagate a
	// cancellation to the server's request context (verified against this
	// toolchain — GET and body-less POST do, POST-with-body doesn't), so
	// gating on it here would hang httptest.Server.Close() forever. The test
	// still models the real scenario — the backend keeps "generating" after
	// the client has given up — it just ends the simulation itself instead
	// of depending on server-side cancellation detection.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-release
	}))
	defer func() {
		close(release)
		srv.Close()
	}()

	st := store.NewTest(t)
	ctx := context.Background()
	if err := st.PutSummaryTimeoutSeconds(ctx, 10); err != nil { // clamped floor
		t.Fatal(err)
	}

	sum := summarize.NewOllama(srv.URL, "test-model")
	p := newSummaryTestPoller(t, st, sum)
	p.Config.SummaryTimeoutSecondsFallback = 90 // must lose to the stored 10s

	art := insertSummaryTestArticle(t, st)

	start := time.Now()
	done := make(chan struct{})
	go func() { p.summarizeOne(ctx, art.ID); close(done) }()

	select {
	case <-done:
	case <-time.After(40 * time.Second):
		t.Fatal("summarizeOne never returned — no deadline was applied")
	}
	elapsed := time.Since(start)
	if elapsed > 30*time.Second {
		t.Fatalf("summarizeOne took %s; the 10s stored timeout was not applied", elapsed)
	}

	if got := hits.Load(); got != 1 {
		t.Fatalf("ollama received %d requests for one article; want 1 (the retry must not fire once ctx has expired)", got)
	}

	got, err := st.GetArticle(ctx, art.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummaryModel != "skipped" {
		t.Fatalf("summary_model = %q, want \"skipped\"", got.SummaryModel)
	}
}
