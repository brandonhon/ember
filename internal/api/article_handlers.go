package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/store"
)

func (d *Dependencies) handleListArticles(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	q := r.URL.Query()

	view := q.Get("view")

	atoi := func(key string) int64 {
		v, _ := strconv.ParseInt(q.Get(key), 10, 64)
		return v
	}
	atoB := func(key string) bool {
		v := q.Get(key)
		return v == "1" || v == "true"
	}

	ctx := r.Context()
	now := time.Now()
	feedID := atoi("feed_id")
	categoryID := atoi("category_id")

	// Reading-view window (Today / a feed / a category): admin-configurable,
	// default 24h, capped at the retention window. Articles older than this are
	// kept in the DB (so search can reach them) but not shown or counted here.
	readingWindow := time.Duration(d.Store.ResolveReadingWindowHours(ctx, store.DefaultReadingWindowHours)) * time.Hour

	// freshAfter is the published-after lower bound. The client may pin it via
	// ?fresh_after=; otherwise it's derived per view. The window bounds which
	// articles are *eligible*; the list itself is paged 50 at a time (keyset
	// cursor + "Load more"), so a busy window doesn't dump thousands of rows.
	freshAfter := atoi("fresh_after")
	if freshAfter == 0 {
		switch view {
		case "fresh":
			fw := d.FreshWindow
			if fw <= 0 {
				fw = 6 * time.Hour
			}
			freshAfter = now.Add(-fw).Unix()
		case "today":
			freshAfter = now.Add(-readingWindow).Unix()
		case "unread":
			// All Unread extends back to the user's previous login (clamped to
			// [reading window, retention]) so time away surfaces everything new
			// since.
			freshAfter = d.Store.UnreadCutoff(ctx, u.ID)
		default:
			// A specific feed or category is a reading view too. It uses the
			// same UnreadCutoff as its sidebar badge (the reading window as a
			// floor, extended back to the previous login on absence) so the
			// unread items in the column always match the badge count — never
			// "badge 5, column 2" when you've been away more than a window.
			if feedID > 0 || categoryID > 0 {
				freshAfter = d.Store.UnreadCutoff(ctx, u.ID)
			}
		}
	}

	limit := 50
	if v := q.Get("limit"); v != "" {
		// Clamp in the handler too (the store also caps): each returned row
		// drives a correlated dup_count subquery, so an unbounded limit is a
		// cheap amplification knob without a fronting proxy to rate-limit.
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= store.MaxArticleListLimit {
			limit = n
		}
	}

	// Summary gate = "is AI summarization on?". When on, hide articles the
	// summarizer hasn't stamped yet — uniformly across every view (including
	// Fresh) so badges match lists. When off (no Ollama), nothing is gated and
	// everything shows everywhere. ?all=1 force-bypasses for admin/debug.
	onlySummarized := d.summariesOn() && !atoB("all")
	query := store.ListArticlesQuery{
		View:            view,
		FeedID:          feedID,
		CategoryID:      categoryID,
		BoardID:         atoi("board_id"),
		Unread:          atoB("unread"),
		Starred:         atoB("starred"),
		Later:           atoB("later"),
		FreshAfter:      freshAfter,
		Limit:           limit,
		PublishedBefore: atoi("cursor_pub"),
		IDBefore:        atoi("cursor_id"),
		OnlySummarized:  onlySummarized,
		Tag:             q.Get("tag"),
		// Feed/category columns show read+unread but their sidebar badges count
		// only unread; dedup unread copies the same way the badge does so the
		// unread cards shown always equal the badge.
		DedupUnread: feedID > 0 || categoryID > 0,
	}
	articles, err := d.Store.ListArticles(ctx, u.ID, query)
	if mapStoreError(w, err) {
		return
	}
	// Emit a paging cursor only when the page came back full — a short page is
	// the last one, so a present cursor means "Load more has something." This
	// is what lets the client show/hide the Load-more button correctly.
	meta := map[string]any{}
	if n := len(articles); n > 0 && n == query.Limit {
		last := articles[n-1]
		meta["next_cursor_pub"] = last.PublishedAt
		meta["next_cursor_id"] = last.ID
	}
	writeData(w, http.StatusOK, articles, meta)
}

func (d *Dependencies) handleGetArticle(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	a, err := d.Store.GetArticleForUser(r.Context(), u.ID, id)
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, a, nil)
}

// handleGetArticleCluster returns the cross-feed siblings of the given
// article: same canonical URL cluster, reached via the user's other
// subscriptions. Drives the "Also in N feeds" pill expansion in the UI.
func (d *Dependencies) handleGetArticleCluster(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	siblings, err := d.Store.ListClusterSiblings(r.Context(), u.ID, id)
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, map[string]any{"siblings": siblings}, nil)
}

type setReadReq struct {
	IDs  []int64 `json:"ids"`
	Read bool    `json:"read"`
}

// maxBulkArticleIDs caps how many ids a single read/star/later request can
// touch. Prevents a single 50k-id payload from triggering an enormous SQL
// statement with hundreds of thousands of placeholders.
const maxBulkArticleIDs = 1000

func (d *Dependencies) handleSetRead(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req setReadReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.IDs) > maxBulkArticleIDs {
		writeError(w, http.StatusBadRequest, "bad_request", "too many ids")
		return
	}
	if mapStoreError(w, d.Store.SetRead(r.Context(), u.ID, req.IDs, req.Read)) {
		return
	}
	writeData(w, http.StatusOK, map[string]int{"count": len(req.IDs)}, nil)
}

type setFlagReq struct {
	ID    int64 `json:"id"`
	Value bool  `json:"value"`
}

func (d *Dependencies) handleSetStar(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req setFlagReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if mapStoreError(w, d.Store.SetStarred(r.Context(), u.ID, req.ID, req.Value)) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}

func (d *Dependencies) handleSetLater(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req setFlagReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if mapStoreError(w, d.Store.SetLater(r.Context(), u.ID, req.ID, req.Value)) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}

type markAllReadReq struct {
	FeedID     int64  `json:"feed_id,omitempty"`
	CategoryID int64  `json:"category_id,omitempty"`
	BoardID    int64  `json:"board_id,omitempty"`
	View       string `json:"view,omitempty"`
}

func (d *Dependencies) handleMarkAllRead(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req markAllReadReq
	if !decodeJSON(w, r, &req) {
		return
	}
	// Boards take a different path — board_articles join, not subscription scope.
	if req.BoardID > 0 {
		n, err := d.Store.MarkBoardRead(r.Context(), u.ID, req.BoardID)
		if mapStoreError(w, err) {
			return
		}
		writeData(w, http.StatusOK, map[string]int64{"count": n}, nil)
		return
	}
	var freshAfter int64
	switch req.View {
	case "fresh":
		fw := d.FreshWindow
		if fw <= 0 {
			fw = 6 * time.Hour
		}
		freshAfter = time.Now().Add(-fw).Unix()
	case "today":
		rw := time.Duration(d.Store.ResolveReadingWindowHours(r.Context(), store.DefaultReadingWindowHours)) * time.Hour
		freshAfter = time.Now().Add(-rw).Unix()
	case "unread":
		freshAfter = d.Store.UnreadCutoff(r.Context(), u.ID)
	}
	n, err := d.Store.MarkAllRead(r.Context(), u.ID, req.FeedID, req.CategoryID, freshAfter)
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, map[string]int64{"count": n}, nil)
}

// handleReExtractArticle re-runs readability extraction against the article's
// URL and persists the result if it's better than what's currently stored.
// Auth-required; any subscriber of the article's feed can trigger it. Content
// updates are shared across users — this fixes the article for everyone, not
// just the caller, which matches how original ingest works.
func (d *Dependencies) handleReExtractArticle(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	// 404 if the user isn't subscribed to the article's feed (or it doesn't
	// exist). GetArticleForUser does the subscription check.
	if _, err := d.Store.GetArticleForUser(r.Context(), u.ID, id); mapStoreError(w, err) {
		return
	}
	if d.Poller == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "extraction not available in this build")
		return
	}
	err := d.Poller.ExtractArticle(r.Context(), id)
	if errors.Is(err, store.ErrNoNewContent) {
		// Same as a successful no-op. The UI surfaces "no_change" so it can
		// disable the button or show a tooltip.
		writeData(w, http.StatusOK, map[string]any{"status": "no_change"}, nil)
		return
	}
	if mapStoreError(w, err) {
		return
	}
	// Echo the now-updated article so the SPA can swap the body in-place.
	a, err := d.Store.GetArticleForUser(r.Context(), u.ID, id)
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, a, map[string]any{"status": "ok"})
}
