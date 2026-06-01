package api

import (
	"net/http"

	"github.com/brandonhon/ember/internal/auth"
)

// handleGetInbox returns the user's email-inbox address (creating it
// on first call). Includes an `enabled` flag — false when the server
// hasn't configured EMBER_EMAIL_DOMAIN, in which case the SPA shows a
// "Ask your admin to configure email" notice.
func (d *Dependencies) handleGetInbox(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	if d.EmailDomain == "" {
		writeData(w, http.StatusOK, map[string]any{"enabled": false}, nil)
		return
	}
	inbox, err := d.Store.EnsureInbox(r.Context(), u.ID)
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, map[string]any{
		"enabled": true,
		"address": inbox.Handle + "@" + d.EmailDomain,
		"feed_id": inbox.FeedID,
	}, nil)
}

// handleRotateInbox regenerates the user's handle. The previous handle
// continues to accept mail for 7 days so the user can update their
// subscriptions without losing in-flight newsletters.
func (d *Dependencies) handleRotateInbox(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	if d.EmailDomain == "" {
		writeError(w, http.StatusServiceUnavailable, "email_disabled", "email inbox is not configured")
		return
	}
	// Ensure the user has an inbox before rotating (calling rotate on a
	// never-created inbox would 404).
	if _, err := d.Store.EnsureInbox(r.Context(), u.ID); mapStoreError(w, err) {
		return
	}
	inbox, err := d.Store.RotateInbox(r.Context(), u.ID)
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, map[string]any{
		"address": inbox.Handle + "@" + d.EmailDomain,
		"feed_id": inbox.FeedID,
	}, nil)
}
