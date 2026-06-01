package api

import (
	"net/http"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/push"
	"github.com/brandonhon/ember/internal/urlcheck"
)

// handleGetVapidKey returns the public VAPID key the SPA needs to call
// pushManager.subscribe. Returns 503 when push is unconfigured.
func (d *Dependencies) handleGetVapidKey(w http.ResponseWriter, r *http.Request) {
	if d.Push == nil {
		writeError(w, http.StatusServiceUnavailable, "push_disabled", "push notifications are not configured")
		return
	}
	writeData(w, http.StatusOK, map[string]string{"public_key": d.Push.PublicKey()}, nil)
}

type createPushSubscriptionReq struct {
	Endpoint  string `json:"endpoint"`
	P256dh    string `json:"p256dh"`
	Auth      string `json:"auth"`
	UserAgent string `json:"user_agent"`
}

// handleCreatePushSubscription registers a browser subscription against
// the authenticated user. Returns the row id.
func (d *Dependencies) handleCreatePushSubscription(w http.ResponseWriter, r *http.Request) {
	if d.Push == nil {
		writeError(w, http.StatusServiceUnavailable, "push_disabled", "push notifications are not configured")
		return
	}
	u, _ := auth.FromContext(r.Context())
	var req createPushSubscriptionReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Endpoint == "" || req.P256dh == "" || req.Auth == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "endpoint, p256dh, auth required")
		return
	}
	// SSRF guard: the endpoint is fetched server-side on every push fan-out,
	// so a malicious value (e.g. http://169.254.169.254/...) would let an
	// authenticated user probe internal services. Reject private/loopback
	// targets at registration; redirects are re-checked by the push client.
	if err := urlcheck.Check(r.Context(), req.Endpoint, d.AllowPrivateURLs); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "endpoint rejected: "+err.Error())
		return
	}
	id, err := d.Store.CreatePushSubscription(r.Context(), u.ID, req.Endpoint, req.P256dh, req.Auth, req.UserAgent)
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, map[string]int64{"id": id}, nil)
}

// handleListPushSubscriptions returns the registered devices for the
// user. Secrets (endpoint / p256dh / auth) are not exposed — only id,
// user_agent, created_at.
func (d *Dependencies) handleListPushSubscriptions(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	subs, err := d.Store.ListPushSubscriptions(r.Context(), u.ID)
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, subs, nil)
}

// handleDeletePushSubscription revokes a registered device. Idempotent
// from the client's perspective — a missing row returns 404 ErrNotFound.
func (d *Dependencies) handleDeletePushSubscription(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	if mapStoreError(w, d.Store.DeletePushSubscription(r.Context(), u.ID, id)) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}

// handleTestPushNotification fires a sample push to all of the user's
// registered subscriptions. Useful for verifying setup after enabling
// notifications.
func (d *Dependencies) handleTestPushNotification(w http.ResponseWriter, r *http.Request) {
	if d.Push == nil {
		writeError(w, http.StatusServiceUnavailable, "push_disabled", "push notifications are not configured")
		return
	}
	u, _ := auth.FromContext(r.Context())
	sent, removed := d.Push.NotifyUser(r.Context(), u.ID, push.Payload{
		Title: "Ember test",
		Body:  "Notifications are working.",
	})
	writeData(w, http.StatusOK, map[string]int{"sent": sent, "removed": removed}, nil)
}
