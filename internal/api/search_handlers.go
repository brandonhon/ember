package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/store"
)

// Handler-side bounds on search paging. The store also clamps the limit, but
// guarding here keeps the contract explicit and caps the offset the store
// otherwise passes straight to SQLite.
const (
	maxSearchLimit  = 100
	maxSearchOffset = 10000
)

func (d *Dependencies) handleSearch(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	q := r.URL.Query()
	query := q.Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "q required")
		return
	}
	limit := 25
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = min(n, maxSearchLimit)
		}
	}
	// offset pages the ranked results 25 at a time for the SPA's "Load more".
	// Cap it so a caller can't make SQLite skip an unbounded row count.
	offset := 0
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = min(n, maxSearchOffset)
		}
	}
	// Search window: default 48h, admin-extendable up to the retention cap.
	// You can't search past what's retained — that's the safeguard.
	hours := d.Store.ResolveSearchWindowHours(r.Context(), store.DefaultSearchWindowHours)
	publishedAfter := time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
	hits, err := d.Store.Search(r.Context(), u.ID, query, limit, publishedAfter, offset)
	if mapStoreError(w, err) {
		return
	}
	// next_offset lets the SPA request the following page; present only when
	// the page came back full (a short page is the last one).
	meta := map[string]any{}
	if len(hits) == limit {
		meta["next_offset"] = offset + limit
	}
	for i := range hits {
		hits[i].ImageURL = d.img.rewrite(hits[i].ImageURL)
	}
	writeData(w, http.StatusOK, hits, meta)
}
