package api

import (
	"context"
	"net/http"
	"time"
)

// handleReadyz pings the database and reports overall readiness.
func (d *Dependencies) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := d.Store.DB.PingContext(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ready": false,
			"err":   err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ready": true})
}
