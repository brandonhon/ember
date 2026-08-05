package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/feed"
	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
	"github.com/brandonhon/ember/internal/ttrss"
	"github.com/brandonhon/ember/internal/urlcheck"
)

type addFeedReq struct {
	URL        string `json:"url"`
	CategoryID *int64 `json:"category_id,omitempty"`
}

type updateFeedReq struct {
	TitleOverride *string `json:"title_override,omitempty"`
	CategoryID    *int64  `json:"category_id,omitempty"`
	ClearCategory bool    `json:"clear_category,omitempty"`
	Muted         *bool   `json:"muted,omitempty"`
	// Summarize opts this subscription in/out of AI summaries (issue #163).
	Summarize *bool `json:"summarize,omitempty"`
	// URL, when set, re-points the subscription to a new source. Validated +
	// SSRF-checked + discovered like add-feed; the shared feed row is never
	// mutated in place (other subscribers keep theirs).
	URL *string `json:"url,omitempty"`
}

func (d *Dependencies) handleListFeeds(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	// Sidebar per-feed badges: unread since the user's previous login (clamped
	// to [1d, retention]) and gated on the summary marker when AI is on, so a
	// badge agrees with the article list.
	cutoff := d.Store.UnreadCutoff(r.Context(), u.ID)
	feeds, err := d.Store.ListFeedsForUser(r.Context(), u.ID, cutoff, d.summariesOn(), d.summaryGraceBefore(r.Context()))
	if mapStoreError(w, err) {
		return
	}
	// Cross-feed dedup the per-feed badges so a duplicate story is counted once
	// (owned by its lowest-id feed) and the per-feed badges sum to All-Unread.
	// ListFeedsForUser stays non-deduped for its other callers (Fever, OPML,
	// ttrss, starter-pack); only this SPA endpoint overlays the deduped counts.
	deduped, err := d.Store.CountUnreadByFeed(r.Context(), u.ID, store.ListArticlesQuery{
		FreshAfter: cutoff, OnlySummarized: d.summariesOn(),
		SummaryGraceBefore: d.summaryGraceBefore(r.Context()),
	})
	if mapStoreError(w, err) {
		return
	}
	for i := range feeds {
		feeds[i].Unread = deduped[feeds[i].ID]
	}
	writeData(w, http.StatusOK, feeds, nil)
}

func (d *Dependencies) handleAddFeed(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req addFeedReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "url required")
		return
	}
	// Let the user omit the scheme: prepend https:// (and upgrade an explicit
	// http://) before validation so "example.com/feed" just works.
	req.URL = feed.NormalizeInputURL(req.URL)
	// SSRF-check, then discover: if the user pasted a website URL (not a feed
	// URL), resolve it to the feed the page advertises.
	target, ok := d.resolveFeedURL(w, r, req.URL)
	if !ok {
		return
	}
	f, err := d.Store.UpsertFeed(r.Context(), models.Feed{URL: target, Title: target})
	if mapStoreError(w, err) {
		return
	}
	sub, err := d.Store.Subscribe(r.Context(), models.Subscription{
		UserID: u.ID, FeedID: f.ID, CategoryID: req.CategoryID,
	})
	if mapStoreError(w, err) {
		return
	}
	// Initial refresh: do it inline (cheap with mocked poller in tests; real
	// poller will fire fetch+parse synchronously, which is fine for a single
	// feed — caller is already paying a network cost). We use the server-
	// level background context (cancelled at shutdown) rather than the
	// request context so a slow client disconnect doesn't abort the fetch.
	if d.Poller != nil {
		_ = d.Poller.RefreshFeed(d.backgroundCtx(), f.ID)
	}
	writeData(w, http.StatusCreated, map[string]any{"feed": f, "subscription": sub}, nil)
}

type discoverReq struct {
	URL string `json:"url"`
}

// handleDiscoverFeeds returns every feed a site advertises without
// subscribing. The add-feed UI calls this first; when a page exposes more
// than one feed it shows a picker, then POSTs the chosen URLs to /api/feeds.
// Returns 200 with {"feeds": []} when the page loads but advertises no feed.
func (d *Dependencies) handleDiscoverFeeds(w http.ResponseWriter, r *http.Request) {
	var req discoverReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "url required")
		return
	}
	req.URL = feed.NormalizeInputURL(req.URL)
	if err := urlcheck.Check(r.Context(), req.URL, d.AllowPrivateURLs); err != nil {
		slog.Default().Info("api: discover URL rejected", "url", req.URL, "reason", err)
		writeError(w, http.StatusBadRequest, "bad_request", "URL is not allowed")
		return
	}
	dctx, cancel := context.WithTimeout(r.Context(), feedDiscoveryTimeout)
	defer cancel()
	disco, validate := d.discoveryClient(dctx)
	feeds, err := feed.DiscoverAll(dctx, disco, req.URL, validate)
	if err != nil {
		slog.Default().Info("api: discover failed", "url", req.URL, "reason", err)
		writeError(w, http.StatusBadGateway, "discover_failed", "could not load URL")
		return
	}
	if feeds == nil {
		feeds = []feed.Discovered{}
	}
	writeData(w, http.StatusOK, map[string]any{"feeds": feeds}, nil)
}

func (d *Dependencies) handleUpdateFeed(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	var req updateFeedReq
	if !decodeJSON(w, r, &req) {
		return
	}
	// category_id 0 (or negative) is not a category row: it reaches SQLite as an
	// FK violation and surfaces as a 500 for what is really a bad request.
	// Removing a folder is clear_category, not category_id 0.
	if req.CategoryID != nil && *req.CategoryID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "category_id must be a positive category id; use clear_category to remove the folder")
		return
	}
	// Source-URL change: resolve + validate the new URL, then re-point this
	// subscription at it. Done before the metadata patch so a bad URL fails
	// without half-applying.
	if req.URL != nil {
		if newURL := feed.NormalizeInputURL(*req.URL); newURL != "" {
			target, ok := d.resolveFeedURL(w, r, newURL)
			if !ok {
				return
			}
			f, err := d.Store.UpsertFeed(r.Context(), models.Feed{URL: target, Title: target})
			if mapStoreError(w, err) {
				return
			}
			if err := d.Store.RepointSubscriptionFeed(r.Context(), u.ID, id, f.ID); err != nil {
				if errors.Is(err, store.ErrConflict) {
					writeError(w, http.StatusConflict, "conflict", "you're already subscribed to that feed")
					return
				}
				if mapStoreError(w, err) {
					return
				}
			}
			if d.Poller != nil {
				_ = d.Poller.RefreshFeed(d.backgroundCtx(), f.ID)
			}
		}
	}
	// Capture the prior opt-out state before the patch: turning summaries back
	// ON should backfill the articles that were stamped 'excluded' while off,
	// otherwise the toggle appears to do nothing until new articles arrive.
	var wasOptedOut bool
	if req.Summarize != nil && *req.Summarize {
		if prior, err := d.Store.GetSubscriptionByID(r.Context(), u.ID, id); err == nil {
			wasOptedOut = !prior.Summarize
		}
	}
	patch := store.UpdateSubscriptionPatch{
		TitleOverride: req.TitleOverride,
		CategoryID:    req.CategoryID,
		ClearCategory: req.ClearCategory,
		Muted:         req.Muted,
		Summarize:     req.Summarize,
	}
	if mapStoreError(w, d.Store.UpdateSubscription(r.Context(), u.ID, id, patch)) {
		return
	}
	// Re-enqueue what was skipped while this feed was opted out. Best-effort:
	// the subscription change already succeeded, and enqueuePendingSummaries
	// picks up anything the queue drops.
	if wasOptedOut {
		if sub, err := d.Store.GetSubscriptionByID(r.Context(), u.ID, id); err == nil {
			if ids, err := d.Store.ResetExcludedByFeed(r.Context(), sub.FeedID); err != nil {
				slog.Default().Warn("feeds: reset excluded summaries", "feed_id", sub.FeedID, "err", err)
			} else if n := d.enqueueSummaries(ids); n > 0 {
				slog.Default().Info("feeds: re-enqueued summaries after opt-in", "feed_id", sub.FeedID, "count", n)
			}
		}
	}
	writeOK(w)
}

// feedDiscoveryTimeout bounds a discovery fetch — the initial page load plus
// any probed feed paths.
const feedDiscoveryTimeout = 10 * time.Second

// discoveryClient builds the SSRF-guarded HTTP client used for feed discovery
// along with the validator applied to every redirect hop and to any URL the
// page advertises. ctx bounds the whole discovery attempt.
func (d *Dependencies) discoveryClient(ctx context.Context) (*http.Client, func(string) error) {
	validate := func(rawURL string) error { return urlcheck.Check(ctx, rawURL, d.AllowPrivateURLs) }
	return &http.Client{
		Timeout:       feedDiscoveryTimeout,
		Transport:     urlcheck.GuardedTransport(d.AllowPrivateURLs),
		CheckRedirect: feed.RedirectGuard(validate),
	}, validate
}

// resolveFeedURL validates a candidate feed URL (SSRF guard) and runs feed
// discovery, returning the concrete feed URL to subscribe to. Discover()
// returns the input unchanged when it already points at a feed. On rejection
// it writes the error response and returns ok=false. Shared by add-feed and
// the edit-feed URL change so both apply the same guards.
func (d *Dependencies) resolveFeedURL(w http.ResponseWriter, r *http.Request, rawURL string) (string, bool) {
	if err := urlcheck.Check(r.Context(), rawURL, d.AllowPrivateURLs); err != nil {
		slog.Default().Info("api: feed URL rejected", "url", rawURL, "reason", err)
		writeError(w, http.StatusBadRequest, "bad_request", "URL is not allowed")
		return "", false
	}
	dctx, cancel := context.WithTimeout(r.Context(), feedDiscoveryTimeout)
	defer cancel()
	disco, validate := d.discoveryClient(dctx)
	discovered, derr := feed.Discover(dctx, disco, rawURL, validate)
	if derr != nil || discovered == "" {
		return rawURL, true
	}
	// feed.Discover's alternate-link path can return a URL it never validated,
	// so re-check what came back before we subscribe to it.
	if err := validate(discovered); err != nil {
		slog.Default().Info("api: discovered feed URL rejected", "url", discovered, "reason", err)
		writeError(w, http.StatusBadRequest, "bad_request", "URL is not allowed")
		return "", false
	}
	return discovered, true
}

// enqueueSummaries pushes article ids onto the summarizer queue and reports how
// many were accepted. Returns 0 when no poller is wired (tests, summaries off).
func (d *Dependencies) enqueueSummaries(ids []int64) int {
	if d.Poller == nil {
		return 0
	}
	enqueued := 0
	for _, id := range ids {
		if d.Poller.EnqueueSummary(id) {
			enqueued++
		}
	}
	return enqueued
}

// subscriptionForParam resolves the {id} path param to one of the caller's
// subscriptions. Writes 400/404/500 and returns ok=false on failure, so a
// foreign or unknown id can never reach the feed-level actions.
func (d *Dependencies) subscriptionForParam(w http.ResponseWriter, r *http.Request, userID int64) (models.Subscription, bool) {
	id, ok := paramInt(w, r, "id")
	if !ok {
		return models.Subscription{}, false
	}
	sub, err := d.Store.GetSubscriptionByID(r.Context(), userID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "feed not found")
		return models.Subscription{}, false
	}
	if err != nil {
		internalError(w, "internal", err)
		return models.Subscription{}, false
	}
	return sub, true
}

// uploadedFile caps the request body, parses the multipart form, and returns
// its "file" part. Writes the 400 and returns ok=false on failure.
func uploadedFile(w http.ResponseWriter, r *http.Request, maxBody int64) (multipart.File, bool) {
	// ParseMultipartForm's argument is the in-memory threshold (parts spill to
	// disk above it), not a body limit; MaxBytesReader enforces the ceiling.
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		slog.Default().Info("api: upload read failed", "path", r.URL.Path, "err", err)
		writeError(w, http.StatusBadRequest, "bad_request", "could not read the uploaded file")
		return nil, false
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "multipart file 'file' required")
		return nil, false
	}
	return file, true
}

func (d *Dependencies) handleDeleteFeed(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	if mapStoreError(w, d.Store.Unsubscribe(r.Context(), u.ID, id)) {
		return
	}
	writeOK(w)
}

func (d *Dependencies) handleRefreshFeed(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	// Resolve subscription id → feed id. Rejects cross-user.
	sub, ok := d.subscriptionForParam(w, r, u.ID)
	if !ok {
		return
	}
	if d.Poller != nil {
		if err := d.Poller.RefreshFeed(r.Context(), sub.FeedID); err != nil {
			internalError(w, "internal", err)
			return
		}
	}
	writeOK(w)
}

// refreshAllSem bounds how many "refresh all" walkers run concurrently across
// all callers. The expensive limiter caps the request rate per IP, but without
// this each accepted request would spawn a goroutine that hammers the shared
// SQLite writer outside the poller's worker pool.
var refreshAllSem = make(chan struct{}, 3)

// handleRefreshAllFeeds kicks an immediate fetch of every feed the user is
// subscribed to (the "Refresh feeds now" button). Each refresh is network-
// bound, so they run in a detached goroutine and the handler returns 202
// straight away; newly-ingested articles surface via the next poll/merge.
func (d *Dependencies) handleRefreshAllFeeds(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	feeds, err := d.Store.ListFeedsForUser(r.Context(), u.ID, 0, false, 0)
	if mapStoreError(w, err) {
		return
	}
	if d.Poller != nil {
		ctx := d.backgroundCtx()
		ids := make([]int64, len(feeds))
		for i, f := range feeds {
			ids[i] = f.ID
		}
		go func() {
			refreshAllSem <- struct{}{}
			defer func() { <-refreshAllSem }()
			for _, id := range ids {
				if err := d.Poller.RefreshFeed(ctx, id); err != nil {
					slog.Default().Warn("refresh-all: feed refresh failed", "feed_id", id, "err", err)
				}
			}
		}()
	}
	writeData(w, http.StatusAccepted, map[string]int{"feeds": len(feeds)}, nil)
}

// handleResummarizeFeed clears the 'skipped' summary marker on every article
// in the feed and re-enqueues each one for summarization. Used when the
// summarizer was previously unavailable (Ollama down, model missing) and
// you want to retry now that it's working.
func (d *Dependencies) handleResummarizeFeed(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	sub, ok := d.subscriptionForParam(w, r, u.ID)
	if !ok {
		return
	}
	ids, err := d.Store.ResetSummariesByFeed(r.Context(), sub.FeedID)
	if err != nil {
		internalError(w, "internal", err)
		return
	}
	writeData(w, http.StatusOK, map[string]int{"reset": len(ids), "enqueued": d.enqueueSummaries(ids)}, nil)
}

// handleResummarizeAll clears summary_model on every article in the database
// and re-enqueues them. Used after a prompt or model change so stale-format
// summaries get replaced. Admin-only because it's a heavy operation.
func (d *Dependencies) handleResummarizeAll(w http.ResponseWriter, r *http.Request) {
	ids, err := d.Store.ClearAllSummaries(r.Context())
	if err != nil {
		internalError(w, "internal", err)
		return
	}
	writeData(w, http.StatusOK, map[string]int{"reset": len(ids), "enqueued": d.enqueueSummaries(ids)}, nil)
}

func (d *Dependencies) handleOPMLImport(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	// OPML is a plain subscription list — 8 MiB is far beyond any real export.
	file, ok := uploadedFile(w, r, 8<<20)
	if !ok {
		return
	}
	defer file.Close()

	n, err := d.OPML.Import(r.Context(), u.ID, file)
	if err != nil {
		slog.Default().Info("api: OPML import failed", "err", err)
		writeError(w, http.StatusBadRequest, "bad_request", "could not import OPML — check the file is a valid OPML export")
		return
	}
	writeData(w, http.StatusOK, map[string]int{"imported": n}, nil)
}

func (d *Dependencies) handleTTRSSImport(w http.ResponseWriter, r *http.Request) {
	if d.TTRSS == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "TT-RSS import is not configured")
		return
	}
	u, _ := auth.FromContext(r.Context())
	// TT-RSS exports embed full article HTML and can be large; cap at 50 MiB.
	file, ok := uploadedFile(w, r, 50<<20)
	if !ok {
		return
	}
	defer file.Close()

	res, err := d.TTRSS.Import(r.Context(), u.ID, file)
	if err != nil {
		slog.Default().Info("api: TT-RSS import failed", "err", err)
		writeError(w, http.StatusBadRequest, "bad_request", "could not import — check the file is a valid TT-RSS export")
		return
	}
	writeData(w, http.StatusOK, res, nil)
}

type ttrssAPIReq struct {
	URL            string `json:"url"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	ImportFeeds    bool   `json:"import_feeds"`
	ImportStarred  bool   `json:"import_starred"`
	ImportArchived bool   `json:"import_archived"`
}

// handleTTRSSAPIImport pulls Starred/Archived articles directly from a running
// TT-RSS instance via its JSON API. Credentials are used only for this call.
func (d *Dependencies) handleTTRSSAPIImport(w http.ResponseWriter, r *http.Request) {
	if d.TTRSS == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "TT-RSS import is not configured")
		return
	}
	u, _ := auth.FromContext(r.Context())
	var req ttrssAPIReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.URL == "" || req.Username == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "url and username required")
		return
	}
	// Default to a full migrate (subscriptions + starred + archived) when the
	// client sends no selection flags.
	if !req.ImportFeeds && !req.ImportStarred && !req.ImportArchived {
		req.ImportFeeds, req.ImportStarred, req.ImportArchived = true, true, true
	}
	res, err := d.TTRSS.ImportFromAPI(r.Context(), u.ID, ttrss.APIOptions{
		BaseURL:        req.URL,
		Username:       req.Username,
		Password:       req.Password,
		ImportFeeds:    req.ImportFeeds,
		ImportStarred:  req.ImportStarred,
		ImportArchived: req.ImportArchived,
	})
	if err != nil {
		// Log the full error server-side for diagnosis; return a generic
		// message. Raw net/http / DNS / TLS errors carry the resolved endpoint,
		// internal hostnames, and TLS detail that shouldn't reach the client.
		slog.Default().Warn("ttrss api import failed", "url", req.URL, "err", err)
		writeError(w, http.StatusBadGateway, "import_failed",
			"could not import from TT-RSS — check the URL/credentials and the server logs.")
		return
	}
	writeData(w, http.StatusOK, res, nil)
}

func (d *Dependencies) handleOPMLExport(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var buf bytes.Buffer
	if err := d.OPML.Export(r.Context(), u.ID, &buf); err != nil {
		internalError(w, "internal", err)
		return
	}
	w.Header().Set("Content-Type", "text/x-opml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="ember.opml"`)
	_, _ = w.Write(buf.Bytes())
}
