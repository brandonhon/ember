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

// handleSmartCounts returns the badge counts for the four sidebar smart
// views (Fresh / Starred / Read Later / Shared). Polled by the sidebar
// alongside the unread refresh so the badges stay live.
func (d *Dependencies) handleSmartCounts(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	cutoff := d.Store.UnreadCutoff(r.Context(), u.ID)
	out, err := d.Store.CountSmartViews(r.Context(), u.ID, d.FreshWindow, cutoff, d.summariesOn())
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, out, nil)
}
