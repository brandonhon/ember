package poller

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/feed"
	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
	"github.com/brandonhon/ember/internal/summarize"
)

// fakeFetcher returns a fixed body and tracks call counts.
type fakeFetcher struct {
	body         []byte
	etag         string
	lastModified string
	fail         bool
	notModified  bool
	calls        atomic.Int64
}

func (f *fakeFetcher) Fetch(_ context.Context, _, _, _ string) (feed.FetchResult, error) {
	f.calls.Add(1)
	if f.fail {
		return feed.FetchResult{StatusCode: 500}, errors.New("boom")
	}
	if f.notModified {
		return feed.FetchResult{Changed: false, StatusCode: 304, ETag: f.etag, LastModified: f.lastModified}, nil
	}
	return feed.FetchResult{
		Changed:      true,
		Body:         f.body,
		ETag:         f.etag,
		LastModified: f.lastModified,
		StatusCode:   200,
	}, nil
}

func mkPoller(t *testing.T, ff Fetcher) *Poller {
	t.Helper()
	st := store.NewTest(t)
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(st, ff, summarize.Noop{}, Config{
		Tick:        time.Millisecond,
		Concurrency: 2,
	}, lg)
}

func seedFeed(t *testing.T, st *store.Store) models.Feed {
	t.Helper()
	f, err := st.UpsertFeed(context.Background(), models.Feed{
		URL: "https://test.local/feed", Title: "Test",
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPoller_TickInsertsArticles(t *testing.T) {
	body, err := os.ReadFile("../feed/testdata/sample.rss")
	if err != nil {
		t.Fatal(err)
	}
	ff := &fakeFetcher{body: body, etag: `"v1"`, lastModified: "now"}
	p := mkPoller(t, ff)
	f := seedFeed(t, p.Store)

	p.Tick(context.Background())
	if p.Metrics.NewArticlesTotal.Load() == 0 {
		t.Fatalf("no articles inserted")
	}

	updated, err := p.Store.GetFeed(context.Background(), f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ETag != `"v1"` {
		t.Errorf("etag not stored: %q", updated.ETag)
	}
	if updated.LastFetched == 0 {
		t.Error("last_fetched not updated")
	}
	if updated.NextFetch <= updated.LastFetched {
		t.Errorf("next_fetch (%d) should be after last_fetched (%d)", updated.NextFetch, updated.LastFetched)
	}

	// Second tick should dedup — but next_fetch is in the future so we have
	// to force the cutoff or refresh directly.
	if err := p.RefreshFeed(context.Background(), f.ID); err != nil {
		t.Fatal(err)
	}
	// Counter only goes up on inserts, so it should remain unchanged.
	if got := p.Metrics.NewArticlesTotal.Load(); got != 2 {
		t.Errorf("second tick inserted new rows: NewArticlesTotal=%d, want 2", got)
	}
}

func TestPoller_FetchErrorBacksOff(t *testing.T) {
	ff := &fakeFetcher{fail: true}
	p := mkPoller(t, ff)
	f := seedFeed(t, p.Store)

	p.Tick(context.Background())

	updated, err := p.Store.GetFeed(context.Background(), f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ErrorCount != 1 {
		t.Errorf("error_count = %d", updated.ErrorCount)
	}
	if updated.LastError == "" {
		t.Error("last_error empty")
	}
	if updated.NextFetch <= p.Config.Now().Unix() {
		t.Errorf("next_fetch not backed off")
	}
	if p.Metrics.FetchesErrored.Load() != 1 {
		t.Errorf("FetchesErrored = %d", p.Metrics.FetchesErrored.Load())
	}
}

func TestPoller_NotModifiedSkipsParse(t *testing.T) {
	ff := &fakeFetcher{notModified: true, etag: `"same"`}
	p := mkPoller(t, ff)
	f := seedFeed(t, p.Store)

	p.Tick(context.Background())

	if p.Metrics.NewArticlesTotal.Load() != 0 {
		t.Errorf("304 should yield no new articles")
	}
	updated, _ := p.Store.GetFeed(context.Background(), f.ID)
	if updated.ErrorCount != 0 {
		t.Errorf("304 shouldn't increment error count")
	}
}

func TestPoller_RefreshFeedNotFound(t *testing.T) {
	ff := &fakeFetcher{}
	p := mkPoller(t, ff)
	if err := p.RefreshFeed(context.Background(), 9999); err == nil {
		t.Error("expected error for missing feed")
	}
}

func TestPoller_RunGracefulShutdown(t *testing.T) {
	body, _ := os.ReadFile("../feed/testdata/sample.rss")
	ff := &fakeFetcher{body: body, etag: `"v1"`}
	p := mkPoller(t, ff)
	p.Config.SummaryWorker = true
	_ = seedFeed(t, p.Store)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
	if p.Metrics.TicksTotal.Load() == 0 {
		t.Error("no ticks ran")
	}
}

func TestPoller_ShouldEnrich(t *testing.T) {
	p := &Poller{}
	cases := []struct {
		name string
		a    models.Article
		want bool
	}{
		{"hn-style link list", models.Article{
			URL:         "https://example.test/post",
			ContentText: "Article URL: https://example.test/post Comments URL: https://news.ycombinator.com/item?id=1",
		}, true},
		{"too short", models.Article{
			URL: "https://x.test", ContentText: "Just three words.",
		}, true},
		{"empty url skipped", models.Article{
			URL: "", ContentText: "short",
		}, false},
		{"long real body", models.Article{
			URL:         "https://blog.test/post",
			ContentText: strings.Repeat("This is real article text with substance. ", 20),
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := p.shouldEnrich(c.a); got != c.want {
				t.Errorf("shouldEnrich(%q) = %v, want %v", c.a.ContentText[:min(40, len(c.a.ContentText))], got, c.want)
			}
		})
	}
}

func TestPoller_FiltersApplyAtIngest(t *testing.T) {
	body, err := os.ReadFile("../feed/testdata/sample.rss")
	if err != nil {
		t.Fatal(err)
	}
	ff := &fakeFetcher{body: body}
	p := mkPoller(t, ff)

	// Two users subscribed to the same feed; only Alice has a filter that
	// marks "Hello"-titled articles read.
	alice, _ := p.Store.CreateUser(context.Background(), models.User{Username: "alice", PasswordHash: "h"})
	bob, _ := p.Store.CreateUser(context.Background(), models.User{Username: "bob", PasswordHash: "h"})
	f := seedFeed(t, p.Store)
	_, _ = p.Store.Subscribe(context.Background(), models.Subscription{UserID: alice.ID, FeedID: f.ID})
	_, _ = p.Store.Subscribe(context.Background(), models.Subscription{UserID: bob.ID, FeedID: f.ID})

	_, err = p.Store.CreateFilter(context.Background(), models.Filter{
		UserID: alice.ID, Name: "mark Hello read", Action: "mark_read",
		MatchJSON: `{"field":"title","op":"contains","value":"Hello"}`,
		Enabled:   true,
	})
	if err != nil {
		t.Fatal(err)
	}

	p.Tick(context.Background())

	// The sample.rss has an article titled "Hello world from RSS" — Alice's
	// state row should be is_read=1; Bob's should be untouched.
	aliceUnread, _ := p.Store.CountUnread(context.Background(), alice.ID, 0, 0)
	bobUnread, _ := p.Store.CountUnread(context.Background(), bob.ID, 0, 0)
	if aliceUnread >= bobUnread {
		t.Errorf("expected alice's filter to drop her unread below bob's; alice=%d bob=%d",
			aliceUnread, bobUnread)
	}
}

func TestPoller_SummaryWorkerPersistsSummary(t *testing.T) {
	body, _ := os.ReadFile("../feed/testdata/sample.rss")
	ff := &fakeFetcher{body: body}
	p := mkPoller(t, ff)
	p.Config.SummaryWorker = true
	_ = seedFeed(t, p.Store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	// Wait until at least one summary row is actually persisted with model=noop.
	deadline := time.Now().Add(3 * time.Second)
	var rowCount int
	for time.Now().Before(deadline) {
		_ = p.Store.DB.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM articles WHERE summary_model = 'noop' AND IFNULL(summary,'') <> ''`).Scan(&rowCount)
		if rowCount > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done

	if rowCount == 0 {
		t.Fatalf("no summary rows persisted; SummariesTotal=%d Errored=%d",
			p.Metrics.SummariesTotal.Load(), p.Metrics.SummariesErrored.Load())
	}
}
