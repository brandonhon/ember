package poller

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
	"github.com/brandonhon/ember/internal/summarize"
)

// undatedRSS builds a feed of n undated items whose GUIDs start at offset, so
// each call can hand the poller a batch of genuinely new articles.
func undatedRSS(offset, n int) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><rss version="2.0"><channel><title>Busy</title><link>https://busy.test/</link><description>x</description>`)
	for i := offset; i < offset+n; i++ {
		fmt.Fprintf(&b, `<item><title>Post %d</title><link>https://busy.test/%d</link><guid isPermaLink="false">busy-%d</guid><description>body</description></item>`, i, i, i)
	}
	b.WriteString(`</channel></rss>`)
	return []byte(b.String())
}

// The adaptive interval is computed from feeds.fetch_interval, so it only
// adapts if the computed value is written back. Quiet (304) polls must grow
// it toward MaxInterval, busy polls must shrink it toward the floor, and a
// fetch error must leave it alone (error backoff already shifts the stored
// base by error_count; persisting the shifted value would compound it).
func TestPoller_AdaptiveIntervalIsPersisted(t *testing.T) {
	st := store.NewTest(t)
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)
	const floor = 30 * time.Minute

	f, err := st.UpsertFeed(ctx, models.Feed{URL: "https://busy.test/rss", Title: "Busy"})
	if err != nil {
		t.Fatal(err)
	}
	poll := func(ff Fetcher) models.Feed {
		t.Helper()
		cur, err := st.GetFeed(ctx, f.ID)
		if err != nil {
			t.Fatal(err)
		}
		New(st, ff, summarize.Noop{}, Config{
			Tick: time.Millisecond, Concurrency: 1,
			Now:                 func() time.Time { return now },
			MinIntervalFallback: floor,
		}, lg).fetchAndStore(ctx, cur)
		got, err := st.GetFeed(ctx, f.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	start := f.FetchInterval
	var got models.Feed
	for range 10 {
		got = poll(&fakeFetcher{notModified: true})
	}
	if want := int64(MaxInterval / time.Second); got.FetchInterval != want {
		t.Fatalf("after 10 quiet polls fetch_interval = %ds (started %ds), want ceiling %ds", got.FetchInterval, start, want)
	}

	before := got.FetchInterval
	got = poll(&fakeFetcher{fail: true})
	if got.FetchInterval != before {
		t.Errorf("fetch error changed fetch_interval %d -> %d; error backoff must not be persisted", before, got.FetchInterval)
	}
	got = poll(&fakeFetcher{fail: true})
	if got.FetchInterval != before {
		t.Errorf("second fetch error changed fetch_interval %d -> %d", before, got.FetchInterval)
	}

	for i := range 10 {
		got = poll(&fakeFetcher{body: undatedRSS(i*3, 3)})
	}
	if want := int64(floor / time.Second); got.FetchInterval != want {
		t.Fatalf("after 10 busy polls fetch_interval = %ds, want floor %ds", got.FetchInterval, want)
	}
}
