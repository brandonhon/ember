package api

import (
	"net/http"
	"strconv"

	"github.com/brandonhon/ember/internal/auth"
)

func (d *Dependencies) handleSearch(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	q := r.URL.Query()
	query := q.Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "q required")
		return
	}
	limit := 30
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	hits, err := d.Store.Search(r.Context(), u.ID, query, limit)
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, hits, nil)
}
