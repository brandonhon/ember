package poller

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/feed"
	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
	"github.com/brandonhon/ember/internal/summarize"
)

// garbageFetcher always returns a body that is not parseable as a feed.
type garbageFetcher struct{}

func (garbageFetcher) Fetch(_ context.Context, _, _, _ string) (feed.FetchResult, error) {
	return feed.FetchResult{Changed: true, StatusCode: 200, Body: []byte("this is not a feed")}, nil
}

// A feed that repeatedly fails to PARSE must back off the same way a feed that
// fails to FETCH does. Both increment error_count, so both should widen the
// retry interval — otherwise a permanently-malformed feed is re-requested at
// the floor interval forever, hammering someone else's origin.
func TestParseFailure_BacksOffLikeFetchFailure(t *testing.T) {
	st := store.NewTest(t)
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	now := time.Unix(1_700_000_000, 0)

	newPoller := func(f Fetcher) *Poller {
		return New(st, f, summarize.Noop{}, Config{
			Tick: time.Millisecond, Concurrency: 1,
			Now:                 func() time.Time { return now },
			MinIntervalFallback: 30 * time.Minute,
		}, lg)
	}

	// Seed two feeds: one that won't fetch, one that won't parse.
	ctx := context.Background()
	broken, err := st.UpsertFeed(ctx, models.Feed{URL: "https://parse.test/rss", Title: "parse"})
	if err != nil {
		t.Fatal(err)
	}
	unreachable, err := st.UpsertFeed(ctx, models.Feed{URL: "https://fetch.test/rss", Title: "fetch"})
	if err != nil {
		t.Fatal(err)
	}

	// Drive each through several consecutive failures.
	const rounds = 4
	for range rounds {
		f, err := st.GetFeed(ctx, broken.ID)
		if err != nil {
			t.Fatal(err)
		}
		newPoller(garbageFetcher{}).fetchAndStore(ctx, f)

		f2, err := st.GetFeed(ctx, unreachable.ID)
		if err != nil {
			t.Fatal(err)
		}
		newPoller(&fakeFetcher{fail: true}).fetchAndStore(ctx, f2)
	}

	gotParse, err := st.GetFeed(ctx, broken.ID)
	if err != nil {
		t.Fatal(err)
	}
	gotFetch, err := st.GetFeed(ctx, unreachable.ID)
	if err != nil {
		t.Fatal(err)
	}

	if gotParse.ErrorCount != rounds || gotFetch.ErrorCount != rounds {
		t.Fatalf("error counts: parse=%d fetch=%d, want %d each", gotParse.ErrorCount, gotFetch.ErrorCount, rounds)
	}

	parseDelay := time.Duration(gotParse.NextFetch-now.Unix()) * time.Second
	fetchDelay := time.Duration(gotFetch.NextFetch-now.Unix()) * time.Second
	t.Logf("after %d consecutive failures: parse retries in %v, fetch retries in %v", rounds, parseDelay, fetchDelay)

	if parseDelay <= 30*time.Minute {
		t.Errorf("parse failures never back off: retry in %v (still the floor) after %d failures — "+
			"a permanently-broken feed is re-requested forever", parseDelay, rounds)
	}
	if parseDelay != fetchDelay {
		t.Errorf("parse backoff %v != fetch backoff %v; both paths increment error_count and should widen alike",
			parseDelay, fetchDelay)
	}
}
