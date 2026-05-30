package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// handleReadyz pings the database and reports overall readiness. /readyz is
// public, so the raw DB error (which can include the file path + SQLite code)
// is logged server-side rather than returned to the caller.
func (d *Dependencies) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := d.Store.DB.PingContext(ctx); err != nil {
		slog.Default().Error("readyz: db ping failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]bool{"ready": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ready": true})
}
