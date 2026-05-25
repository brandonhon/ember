package api

import (
	"net/http"

	"github.com/brandonhon/ember/internal/auth"
)

// handleGetStats returns the user's reading-activity snapshot.
func (d *Dependencies) handleGetStats(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	out, err := d.Store.UserStatsSnapshot(r.Context(), u.ID)
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, out, nil)
}
