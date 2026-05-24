package poller

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brandonhon/ember/internal/feed"
	"github.com/brandonhon/ember/internal/filters"
	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
	"github.com/brandonhon/ember/internal/summarize"
)

// Fetcher is the subset of *feed.Fetcher the poller depends on. Defined
// locally so tests can inject a fake.
type Fetcher interface {
	Fetch(ctx context.Context, url, etag, lastModified string) (feed.FetchResult, error)
}

// Config controls poller runtime.
type Config struct {
	Tick          time.Duration
	Concurrency   int
	SummaryQueue  int
	BatchLimit    int
	Now           func() time.Time
	SummaryWorker bool // if false, no async summary worker (tests)
	// EnrichOnIngest controls whether anemic article bodies trigger a
	// readability HTTP fetch against the article URL. Default true in
	// production; tests set false to keep them fast.
	EnrichOnIngest bool
}

// Metrics is an in-memory snapshot for observability/tests.
type Metrics struct {
	TicksTotal       atomic.Int64
	FetchesTotal     atomic.Int64
	FetchesErrored   atomic.Int64
	NewArticlesTotal atomic.Int64
	SummariesTotal   atomic.Int64
	SummariesErrored atomic.Int64
}

// Poller drives feed fetching.
type Poller struct {
	Store      *store.Store
	Fetcher    Fetcher
	Summarizer summarize.Summarizer
	Logger     *slog.Logger
	Config     Config
	Metrics    *Metrics

	summaryCh chan int64 // article IDs awaiting summary
}

// New constructs a Poller.
func New(st *store.Store, f Fetcher, s summarize.Summarizer, cfg Config, lg *slog.Logger) *Poller {
	if cfg.Tick <= 0 {
		cfg.Tick = 60 * time.Second
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 8
	}
	if cfg.SummaryQueue <= 0 {
		cfg.SummaryQueue = 256
	}
	if cfg.BatchLimit <= 0 {
		cfg.BatchLimit = 50
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if lg == nil {
		lg = slog.Default()
	}
	return &Poller{
		Store:      st,
		Fetcher:    f,
		Summarizer: s,
		Logger:     lg,
		Config:     cfg,
		Metrics:    &Metrics{},
		summaryCh:  make(chan int64, cfg.SummaryQueue),
	}
}

// Run starts the poller scheduler and worker pool. Returns when ctx is done.
func (p *Poller) Run(ctx context.Context) {
	var wg sync.WaitGroup

	if p.Config.SummaryWorker && p.Summarizer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.summaryWorker(ctx)
		}()
	}

	ticker := time.NewTicker(p.Config.Tick)
	defer ticker.Stop()

	// Tick once immediately.
	p.Tick(ctx)
	for {
		select {
		case <-ctx.Done():
			close(p.summaryCh)
			wg.Wait()
			return
		case <-ticker.C:
			p.Tick(ctx)
		}
	}
}

// Tick runs one scheduling pass. Selects due feeds and dispatches them on the
// worker pool, waits for them to complete.
func (p *Poller) Tick(ctx context.Context) {
	p.Metrics.TicksTotal.Add(1)
	cutoff := p.Config.Now().Unix()
	due, err := p.Store.FeedsDue(ctx, cutoff, p.Config.BatchLimit)
	if err != nil {
		p.Logger.Error("poller: feeds due query failed", "err", err)
		return
	}
	if len(due) == 0 {
		return
	}
	jobs := make(chan models.Feed, len(due))
	var wg sync.WaitGroup
	for range p.Config.Concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				p.fetchAndStore(ctx, f)
			}
		}()
	}
	for _, f := range due {
		jobs <- f
	}
	close(jobs)
	wg.Wait()
}

// RefreshFeed forces an immediate fetch of a single feed.
func (p *Poller) RefreshFeed(ctx context.Context, feedID int64) error {
	f, err := p.Store.GetFeed(ctx, feedID)
	if err != nil {
		return err
	}
	p.fetchAndStore(ctx, f)
	return nil
}

func (p *Poller) fetchAndStore(ctx context.Context, f models.Feed) {
	p.Metrics.FetchesTotal.Add(1)
	res, err := p.Fetcher.Fetch(ctx, f.URL, f.ETag, f.LastModified)
	now := p.Config.Now()
	if err != nil {
		p.Metrics.FetchesErrored.Add(1)
		errCount := f.ErrorCount + 1
		next := now.Add(AdaptiveInterval(IntervalInputs{
			HadError: true, ErrorCount: errCount,
			Current: time.Duration(f.FetchInterval) * time.Second,
		}))
		_ = p.Store.UpdateFeedFetch(ctx, f.ID, store.UpdateFeedFetchPatch{
			LastFetched: now.Unix(),
			NextFetch:   next.Unix(),
			ErrorCount:  errCount,
			LastError:   err.Error(),
		})
		p.Logger.Warn("poller: fetch failed", "feed_id", f.ID, "url", f.URL, "err", err)
		return
	}

	// 304 — nothing new.
	if !res.Changed {
		next := now.Add(AdaptiveInterval(IntervalInputs{
			NewArticles: 0,
			Current:     time.Duration(f.FetchInterval) * time.Second,
		}))
		_ = p.Store.UpdateFeedFetch(ctx, f.ID, store.UpdateFeedFetchPatch{
			LastFetched: now.Unix(),
			NextFetch:   next.Unix(),
			ErrorCount:  0,
		})
		return
	}

	parsed, err := feed.Parse(ctx, f.ID, res.Body, f.URL)
	if err != nil {
		p.Metrics.FetchesErrored.Add(1)
		_ = p.Store.UpdateFeedFetch(ctx, f.ID, store.UpdateFeedFetchPatch{
			LastFetched: now.Unix(),
			NextFetch:   now.Add(MinInterval).Unix(),
			ErrorCount:  f.ErrorCount + 1,
			LastError:   err.Error(),
		})
		p.Logger.Warn("poller: parse failed", "feed_id", f.ID, "err", err)
		return
	}

	// Resolve subscribers once per feed; reused for filter application below.
	subs, subErr := p.Store.ListSubscriberIDs(ctx, f.ID)
	if subErr != nil {
		p.Logger.Warn("poller: list subscribers", "feed_id", f.ID, "err", subErr)
	}

	var newCount int
	for _, a := range parsed.Articles {
		// If the feed's body is just a link list (HN-style) or too short to
		// be useful, fetch readability against the article URL to extract real
		// content + a lead image. Best-effort: never blocks ingest on failure.
		if p.Config.EnrichOnIngest && p.shouldEnrich(a) {
			p.enrichWithReadability(ctx, &a)
		}
		stored, inserted, err := p.Store.UpsertArticle(ctx, a)
		if err != nil {
			p.Logger.Warn("poller: upsert article failed", "feed_id", f.ID, "guid", a.GUID, "err", err)
			continue
		}
		if !inserted {
			continue
		}
		newCount++
		// Apply each subscriber's filters to the new article.
		for _, userID := range subs {
			p.applyFiltersForUser(ctx, userID, stored)
		}
		// Enqueue for summarization (best-effort; drop if queue full).
		if p.Summarizer != nil {
			select {
			case p.summaryCh <- stored.ID:
			default:
			}
		}
	}
	p.Metrics.NewArticlesTotal.Add(int64(newCount))

	// Title/site_url enrichment on first successful fetch.
	patch := store.UpdateFeedFetchPatch{
		LastFetched: now.Unix(),
		NextFetch: now.Add(AdaptiveInterval(IntervalInputs{
			NewArticles: newCount,
			Current:     time.Duration(f.FetchInterval) * time.Second,
		})).Unix(),
		ErrorCount: 0,
	}
	if res.ETag != "" {
		patch.ETag = ptr(res.ETag)
	}
	if res.LastModified != "" {
		patch.LastModified = ptr(res.LastModified)
	}
	if parsed.Title != "" && f.Title != parsed.Title {
		patch.Title = ptr(parsed.Title)
	}
	if parsed.SiteURL != "" {
		patch.SiteURL = ptr(parsed.SiteURL)
	}
	_ = p.Store.UpdateFeedFetch(ctx, f.ID, patch)
}

// linkListRE matches "Article URL: ... Comments URL: ..." — the canonical
// HN-style RSS body that has no actual article text in it.
var linkListRE = regexp.MustCompile(`(?i)\bcomments\s*url\b|^article\s*url\b`)

// shouldEnrich returns true when the article's parsed body is too thin or
// looks like a link list — readability against the article URL will usually
// produce something more useful.
func (p *Poller) shouldEnrich(a models.Article) bool {
	if a.URL == "" {
		return false
	}
	text := strings.TrimSpace(a.ContentText)
	if len(text) < 200 {
		return true
	}
	if linkListRE.MatchString(text) && len(text) < 800 {
		return true
	}
	return false
}

// enrichWithReadability fetches the article URL through go-readability and
// replaces the parsed body + image_url with the extracted content. Failures
// are logged and the original article is left intact. Re-computes the
// content_hash so dedup works on the enriched body for new articles.
func (p *Poller) enrichWithReadability(ctx context.Context, a *models.Article) {
	// Short per-request timeout so a slow site doesn't stall the whole feed.
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 15 * time.Second}
	r, err := feed.ExtractFromURL(rctx, client, a.URL)
	if err != nil {
		p.Logger.Debug("poller: readability failed", "url", a.URL, "err", err)
		return
	}
	if len(strings.TrimSpace(r.Text)) < len(strings.TrimSpace(a.ContentText)) {
		// Worse than what we already had — keep the original.
		return
	}
	a.ContentHTML = r.HTML
	a.ContentText = r.Text
	if a.ImageURL == "" && r.ImageURL != "" {
		a.ImageURL = r.ImageURL
	}
	a.ContentHash = feed.ContentHash(a.URL, a.Title, a.ContentText)
}

// applyFiltersForUser fetches a user's enabled filters and applies them to the
// just-ingested article. Errors are logged but never fail ingest.
func (p *Poller) applyFiltersForUser(ctx context.Context, userID int64, a models.Article) {
	fs, err := p.Store.ListActiveFilters(ctx, userID)
	if err != nil {
		p.Logger.Warn("poller: list filters", "user_id", userID, "err", err)
		return
	}
	if len(fs) == 0 {
		return
	}
	out := filters.Apply(fs, a)
	if !out.Any() {
		return
	}
	if out.MarkRead {
		if err := p.Store.SetRead(ctx, userID, []int64{a.ID}, true); err != nil {
			p.Logger.Warn("poller: filter mark_read", "user_id", userID, "article_id", a.ID, "err", err)
		}
	}
	if out.Star {
		if err := p.Store.SetStarred(ctx, userID, a.ID, true); err != nil {
			p.Logger.Warn("poller: filter star", "user_id", userID, "article_id", a.ID, "err", err)
		}
	}
}

func (p *Poller) summaryWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case id, ok := <-p.summaryCh:
			if !ok {
				return
			}
			p.summarizeOne(ctx, id)
		}
	}
}

// summarizeOne attempts to summarize one article. On any failure (LLM down,
// empty output, persistence error) we still stamp summary_model='skipped' so
// the article becomes visible in the list — better to show a story without a
// summary card than to hide it forever.
func (p *Poller) summarizeOne(ctx context.Context, articleID int64) {
	p.Metrics.SummariesTotal.Add(1)
	art, err := p.Store.GetArticle(ctx, articleID)
	if err != nil {
		p.Metrics.SummariesErrored.Add(1)
		return
	}
	if p.Summarizer == nil {
		// No summarizer configured — mark skipped so the article still shows.
		p.markSkipped(ctx, articleID)
		return
	}
	bullets, model, err := p.Summarizer.Summarize(ctx, art.Title, art.ContentText)
	if err != nil {
		p.Metrics.SummariesErrored.Add(1)
		p.Logger.Warn("poller: summarize failed", "article_id", articleID, "err", err)
		p.markSkipped(ctx, articleID)
		return
	}
	joined := joinBullets(bullets)
	if joined == "" {
		p.Metrics.SummariesErrored.Add(1)
		p.markSkipped(ctx, articleID)
		return
	}
	if err := p.Store.UpdateSummary(ctx, articleID, joined, model); err != nil {
		p.Metrics.SummariesErrored.Add(1)
		p.Logger.Warn("poller: persist summary", "article_id", articleID, "err", err)
	}
}

// markSkipped writes summary_model='skipped' so the article shows in lists
// even though we couldn't summarize it.
func (p *Poller) markSkipped(ctx context.Context, articleID int64) {
	if err := p.Store.UpdateSummary(ctx, articleID, "", "skipped"); err != nil {
		p.Logger.Warn("poller: mark skipped", "article_id", articleID, "err", err)
	}
}

func joinBullets(bs []string) string {
	out := ""
	for i, b := range bs {
		if i > 0 {
			out += "\n"
		}
		out += "• " + b
	}
	return out
}

func ptr[T any](v T) *T { return &v }
