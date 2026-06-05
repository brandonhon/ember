package poller

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
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
	"github.com/brandonhon/ember/internal/urlcheck"
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
	// DisableImages drops image_url at ingest so the article hero image is
	// never stored. Set via EMBER_DISABLE_IMAGES at install.
	DisableImages bool
	// AllowPrivateURLs disables the SSRF block on outbound fetches.
	AllowPrivateURLs bool
	// InitialBacklogHoursFallback is the env-derived default for the first-
	// ingest backlog window applied when a feed is fetched for the very first
	// time (f.LastFetched == 0). The poller resolves the live value by
	// preferring the app_settings row over this fallback. Set 0 to disable
	// the gate (ingest the feed's full upstream history).
	InitialBacklogHoursFallback int
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
		// Backfill: enqueue any article that doesn't yet have a summary so a
		// restart picks up where the previous process left off. Runs once at
		// startup; the channel buffer caps how many we queue eagerly. Tracked
		// in the same WaitGroup so a shutdown-time send on summaryCh can't
		// race with the channel being closed.
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.enqueuePendingSummaries(ctx)
		}()
	}

	ticker := time.NewTicker(p.Config.Tick)
	defer ticker.Stop()

	// Tick once immediately.
	p.Tick(ctx)
	for {
		select {
		case <-ctx.Done():
			// Don't close summaryCh: request handlers (RefreshFeed,
			// handleAddFeed) may still call EnqueueSummary during the HTTP
			// graceful-shutdown window, and a send on a closed channel
			// panics regardless of any default case. summaryWorker exits on
			// ctx.Done() — closing the channel was redundant.
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

// MetricsSnapshot returns a copy of the poller's atomic counters keyed by
// snake-case names suitable for emission as Prometheus metrics.
func (p *Poller) MetricsSnapshot() map[string]int64 {
	return map[string]int64{
		"ticks_total":         p.Metrics.TicksTotal.Load(),
		"fetches_total":       p.Metrics.FetchesTotal.Load(),
		"fetches_errored":     p.Metrics.FetchesErrored.Load(),
		"new_articles_total":  p.Metrics.NewArticlesTotal.Load(),
		"summaries_total":     p.Metrics.SummariesTotal.Load(),
		"summaries_errored":   p.Metrics.SummariesErrored.Load(),
		"summary_queue_depth": int64(len(p.summaryCh)),
	}
}

// EnqueueSummary best-effort places an article id on the summary queue.
// Returns true if the id was enqueued, false if the queue is full or no
// summarizer is configured.
func (p *Poller) EnqueueSummary(articleID int64) bool {
	if p.Summarizer == nil {
		return false
	}
	select {
	case p.summaryCh <- articleID:
		return true
	default:
		return false
	}
}

// ExtractArticle re-runs the readability extractor against the article's URL
// and overwrites content_text + content_html when extraction yields more
// text. Backs the "Re-extract" button in the reader pane — for feeds whose
// excerpts slipped past shouldEnrich at ingest time. Returns store.ErrNotFound
// when the article doesn't exist; store.ErrNoNewContent when readability ran
// but produced no improvement over what's already stored (the handler maps
// that to a 200 with status=no_change).
func (p *Poller) ExtractArticle(ctx context.Context, articleID int64) error {
	a, err := p.Store.GetArticle(ctx, articleID)
	if err != nil {
		return err
	}
	if a.URL == "" {
		return errors.New("article has no URL to extract from")
	}
	before := strings.TrimSpace(a.ContentText)
	p.enrichWithReadability(ctx, &a)
	after := strings.TrimSpace(a.ContentText)
	if after == before {
		// enrichWithReadability already bails when readability returns less
		// text than we had. Same-length / same-content means nothing to write.
		return store.ErrNoNewContent
	}
	return p.Store.UpdateArticleContent(ctx, articleID, a.ContentText, a.ContentHTML, a.ImageURL)
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

	// Initial-backlog gate: on a feed's first-ever ingest (LastFetched == 0)
	// drop entries published more than N hours ago so adding a long-lived feed
	// doesn't dump months of history into the reader. Subsequent polls of the
	// same feed never apply the gate. Articles missing published_at are kept
	// (a feed that doesn't date its entries is more often "new dropped on us"
	// than "ancient archive"). 0 hours disables the gate entirely.
	var backlogCutoff int64
	if f.LastFetched == 0 {
		hours := p.Store.ResolveBacklogHours(ctx, p.Config.InitialBacklogHoursFallback)
		if hours > 0 {
			backlogCutoff = now.Add(-time.Duration(hours) * time.Hour).Unix()
		}
	}

	var newCount int
	for _, a := range parsed.Articles {
		if backlogCutoff > 0 && a.PublishedAt > 0 && a.PublishedAt < backlogCutoff {
			continue
		}
		// If the feed's body is just a link list (HN-style) or too short to
		// be useful, fetch readability against the article URL to extract real
		// content + a lead image. Best-effort: never blocks ingest on failure.
		if p.Config.EnrichOnIngest && p.shouldEnrich(a) {
			p.enrichWithReadability(ctx, &a)
		}
		// Strip aggregator residue ("Comments", "View Comments", "Read more")
		// no matter how the body got here. Even after enrichment some sites
		// leave a standalone <p>Comments</p> trailing the article.
		if before := a.ContentHTML; before != "" {
			a.ContentHTML = stripCommentsResidue(before)
		}
		if p.Config.DisableImages {
			a.ImageURL = ""
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
		// Enqueue for summarization (best-effort; drop if queue full). When
		// no summarizer is configured (EMBER_DISABLE_SUMMARIES) we stamp the
		// article with summary_model='disabled' so the SPA's OnlySummarized
		// filter passes it through — otherwise new articles would never
		// appear without manual refresh.
		if p.Summarizer != nil {
			select {
			case p.summaryCh <- stored.ID:
			default:
			}
		} else {
			if err := p.Store.UpdateSummary(ctx, stored.ID, "", "disabled"); err != nil {
				p.Logger.Warn("poller: stamp summary_model=disabled", "article_id", stored.ID, "err", err)
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

// aggregatorHosts are link-aggregator sites whose RSS entries don't contain
// the actual article body. When the article URL points to one of these and
// readability fails to extract anything substantial, we fall back to the
// first external link found in the original feed body.
var aggregatorHosts = map[string]bool{
	"lobste.rs":            true,
	"news.ycombinator.com": true,
	"reddit.com":           true,
	"old.reddit.com":       true,
	"www.reddit.com":       true,
	"hckrnews.com":         true,
	"feedproxy.google.com": true,
}

// hrefRE captures URLs from <a href="..."> attributes in raw HTML. Used to
// recover the real article link out of aggregator-style RSS bodies.
var hrefRE = regexp.MustCompile(`(?i)href\s*=\s*"([^"]+)"`)

// commentsResidueInner is the inner content pattern for "comments only"
// snippets — text that is just "Comments", "View Comments", "Read more",
// etc., optionally wrapped in nested tags (like <a>Comments</a>).
const commentsResidueInner = `\s*(?:<[^>]+>\s*)*(?:View\s+)?(?:Read\s+more|Read\s+the\s+rest|Comments?(?:\s*\(\d+\))?|Continue\s+reading|Discuss(?:\s+on\s+\w+)?|See\s+comments)\s*(?:<[^>]+>\s*)*`

// commentsResidueREs are per-tag matchers. Go's RE2 has no backreferences,
// so we can't use `<(p|li|div)>...</\1>`. Instead we apply a regex per tag.
var commentsResidueREs = []*regexp.Regexp{
	regexp.MustCompile(`(?is)<p[^>]*>` + commentsResidueInner + `</p>`),
	regexp.MustCompile(`(?is)<li[^>]*>` + commentsResidueInner + `</li>`),
	regexp.MustCompile(`(?is)<div[^>]*>` + commentsResidueInner + `</div>`),
}

// stripCommentsResidue removes stand-alone "Comments"/"Read more" paragraphs
// from an article HTML body. Safe to run on any content_html: real article
// paragraphs are too long to match the pattern.
func stripCommentsResidue(html string) string {
	for _, re := range commentsResidueREs {
		html = re.ReplaceAllString(html, "")
	}
	return html
}

// hostOf returns the lowercased hostname of a URL, or "" if it can't be parsed.
func hostOf(u string) string {
	if u == "" {
		return ""
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Host)
}

// firstExternalLink scans HTML for the first <a href="..."> whose host is not
// one of the aggregator hosts and not the source host. Returns "" if nothing
// usable is found.
func firstExternalLink(html, sourceURL string) string {
	srcHost := hostOf(sourceURL)
	for _, m := range hrefRE.FindAllStringSubmatch(html, -1) {
		candidate := m[1]
		host := hostOf(candidate)
		if host == "" || host == srcHost {
			continue
		}
		if aggregatorHosts[host] {
			continue
		}
		if !strings.HasPrefix(candidate, "http") {
			continue
		}
		return candidate
	}
	return ""
}

// shouldEnrich returns true when the article's parsed body is too thin or
// looks like a link list — readability against the article URL will usually
// produce something more useful.
//
// Threshold history: original gate was <200, bumped to <400 in PR #48 after
// TheHackerNews / Feedburner excerpts slipped through, then to <600 (this
// change) after a live test confirmed a 396-char excerpt was *just* under
// the previous gate. 600 keeps us safely below where linkListRE takes over
// at <800. enrichWithReadability already bails when readability returns
// less text than we have, so a wrong-positive only costs one HTTP round-trip
// per first-ingest.
func (p *Poller) shouldEnrich(a models.Article) bool {
	if a.URL == "" {
		return false
	}
	text := strings.TrimSpace(a.ContentText)
	if len(text) < 600 {
		return true
	}
	if linkListRE.MatchString(text) && len(text) < 800 {
		return true
	}
	return false
}

// enrichWithReadability fetches the article URL through go-readability and
// replaces the parsed body + image_url with the extracted content. When the
// article URL points to a link aggregator (Lobsters, HN, Reddit), we first
// look for an external link in the feed body and use that instead — the
// aggregator page's "real" content is comments, which isn't what we want.
//
// Failures are logged and the original article is left intact. Re-computes
// the content_hash so dedup works on the enriched body for new articles.
func (p *Poller) enrichWithReadability(ctx context.Context, a *models.Article) {
	target := a.URL
	if aggregatorHosts[hostOf(target)] {
		if ext := firstExternalLink(a.ContentHTML, a.URL); ext != "" {
			target = ext
		} else {
			// Aggregator page with no extractable external link — readability
			// would just re-fetch comments. Skip.
			p.Logger.Debug("poller: aggregator article with no external link, skipping enrich", "url", a.URL)
			return
		}
	}
	// Short per-request timeout so a slow site doesn't stall the whole feed.
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// SSRF check: the target may have come from a feed body (aggregator path)
	// and could be any URL the publisher chose.
	if err := urlcheck.Check(rctx, target, p.Config.AllowPrivateURLs); err != nil {
		p.Logger.Debug("poller: readability target rejected by urlcheck", "url", target, "err", err)
		return
	}
	// Re-validate on every redirect hop: the initial urlcheck only covers
	// `target`, but a publisher-controlled page can 30x to a private/metadata
	// address. Mirror the SSRF guard wired into the feed Fetcher + discovery
	// client.
	client := &http.Client{
		Timeout:   15 * time.Second,
		Transport: urlcheck.GuardedTransport(p.Config.AllowPrivateURLs),
		CheckRedirect: feed.RedirectGuard(func(rawURL string) error {
			return urlcheck.Check(rctx, rawURL, p.Config.AllowPrivateURLs)
		}),
	}
	r, err := feed.ExtractFromURL(rctx, client, target)
	if err != nil {
		p.Logger.Debug("poller: readability failed", "url", target, "err", err)
		return
	}
	cleanHTML := stripCommentsResidue(r.HTML)
	cleanText := strings.TrimSpace(r.Text)
	if len(cleanText) < len(strings.TrimSpace(a.ContentText)) {
		// Worse than what we already had — keep the original.
		return
	}
	a.ContentHTML = cleanHTML
	a.ContentText = cleanText
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

// enqueuePendingSummaries reads articles with no summary_model from the store
// and pushes them onto the summary worker channel. Bounded by the channel
// buffer; runs in its own goroutine so it never blocks startup.
func (p *Poller) enqueuePendingSummaries(ctx context.Context) {
	ids, err := p.Store.ListUnsummarizedIDs(ctx, p.Config.SummaryQueue)
	if err != nil {
		p.Logger.Warn("poller: backfill summary queue", "err", err)
		return
	}
	if len(ids) == 0 {
		return
	}
	p.Logger.Info("poller: backfilling summary queue", "pending", len(ids))
	for _, id := range ids {
		select {
		case <-ctx.Done():
			return
		case p.summaryCh <- id:
		default:
			return
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
	res, model, err := p.Summarizer.Summarize(ctx, art.Title, art.ContentText)
	if err != nil {
		p.Metrics.SummariesErrored.Add(1)
		p.Logger.Warn("poller: summarize failed", "article_id", articleID, "err", err)
		p.markSkipped(ctx, articleID)
		return
	}
	joined := joinResult(res)
	if joined == "" {
		p.Metrics.SummariesErrored.Add(1)
		p.markSkipped(ctx, articleID)
		return
	}
	if err := p.Store.UpdateSummary(ctx, articleID, joined, model); err != nil {
		p.Metrics.SummariesErrored.Add(1)
		p.Logger.Warn("poller: persist summary", "article_id", articleID, "err", err)
	}
	// Persist the cleaned body if the model returned one. Wrapped in
	// paragraphs so the Reader's prose styling kicks in (the LLM emits plain
	// text, not HTML).
	if cleaned := strings.TrimSpace(res.Cleaned); cleaned != "" {
		// cleaned_html is rendered via {@html}; sanitize the model output (a
		// prompt-injected feed could coax HTML out of the summarizer) before it
		// is paragraphized and stored.
		html := feed.SanitizeHTML(paragraphizePlain(cleaned))
		if err := p.Store.UpdateCleanedHTML(ctx, articleID, html); err != nil {
			p.Logger.Warn("poller: persist cleaned_html", "article_id", articleID, "err", err)
		}
	}
}

// paragraphizePlain wraps blank-line-separated chunks of plain text in <p>
// tags. The summarizer returns CLEANED as prose, not HTML, so we re-introduce
// paragraph breaks for the Reader's body styling. HTML special characters
// are escaped to keep this safe to render with {@html}.
func paragraphizePlain(s string) string {
	chunks := strings.Split(s, "\n\n")
	var b strings.Builder
	for _, c := range chunks {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		// Escape & < > and convert single newlines inside a paragraph to <br>.
		c = htmlEscape(c)
		c = strings.ReplaceAll(c, "\n", "<br>")
		b.WriteString("<p>")
		b.WriteString(c)
		b.WriteString("</p>")
	}
	return b.String()
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

// markSkipped writes summary_model='skipped' so the article shows in lists
// even though we couldn't summarize it.
func (p *Poller) markSkipped(ctx context.Context, articleID int64) {
	if err := p.Store.UpdateSummary(ctx, articleID, "", "skipped"); err != nil {
		p.Logger.Warn("poller: mark skipped", "article_id", articleID, "err", err)
	}
}

// joinResult flattens a summarize.Result into the stored summary text:
// the paragraph followed by a blank line followed by one "• " bullet per line.
// The reader splits on the first "• " line to recover the structure.
func joinResult(r summarize.Result) string {
	var b strings.Builder
	para := strings.TrimSpace(r.Paragraph)
	if para != "" {
		b.WriteString(para)
	}
	if len(r.Bullets) > 0 {
		if para != "" {
			b.WriteString("\n\n")
		}
		for i, bullet := range r.Bullets {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString("• ")
			b.WriteString(bullet)
		}
	}
	return b.String()
}

func ptr[T any](v T) *T { return &v }
