package api

import (
	"net/http"
	"strconv"
	"time"
)

// keySessionTTL is the app_settings key that persists an admin-configured
// session TTL across restarts. Value is seconds, stored as decimal string.
// Empty (or unset) means "fall through to the env-var / DefaultSessionTTL
// that auth.New picked at boot".
const keySessionTTL = "session_ttl_seconds"

// Allowed range: 5 minutes lower bound (no foot-guns), 90 days upper bound
// (matches the original hardcoded 30d max-plus-room).
const (
	minSessionTTL = 5 * time.Minute
	maxSessionTTL = 90 * 24 * time.Hour
)

type sessionTTLResponse struct {
	TTLSeconds int64 `json:"ttl_seconds"`
	// Source explains where the active TTL came from. Useful in the UI
	// to disambiguate "current value is from EMBER_SESSION_TTL" vs "set
	// in the admin UI" vs "default fallback".
	Source string `json:"source"`
}

// handleGetSessionTTL returns the currently-active session TTL plus a hint
// about where it came from. Admin-only.
func (d *Dependencies) handleGetSessionTTL(w http.ResponseWriter, r *http.Request) {
	ttl := d.Auth.SessionTTL
	source := "default"
	if v, _ := d.Store.GetAppSetting(r.Context(), keySessionTTL); v != "" {
		source = "admin"
	}
	writeData(w, http.StatusOK, sessionTTLResponse{
		TTLSeconds: int64(ttl.Seconds()),
		Source:     source,
	}, nil)
}

type setSessionTTLReq struct {
	TTLSeconds int64 `json:"ttl_seconds"`
}

// handleSetSessionTTL persists a new session TTL to app_settings and applies
// it to the in-memory Auth so newly-issued cookies pick it up immediately.
// Existing DB sessions keep their stored expires_at — only new logins (and
// re-logins after the existing cookie expires) get the new TTL.
//
// Admin-only. Range-validated.
func (d *Dependencies) handleSetSessionTTL(w http.ResponseWriter, r *http.Request) {
	var req setSessionTTLReq
	if !decodeJSON(w, r, &req) {
		return
	}
	d2 := time.Duration(req.TTLSeconds) * time.Second
	if d2 < minSessionTTL || d2 > maxSessionTTL {
		writeError(w, http.StatusBadRequest, "bad_request",
			"ttl_seconds must be between "+strconv.FormatInt(int64(minSessionTTL.Seconds()), 10)+
				" and "+strconv.FormatInt(int64(maxSessionTTL.Seconds()), 10))
		return
	}
	if err := d.Store.PutAppSetting(r.Context(), keySessionTTL,
		strconv.FormatInt(int64(d2.Seconds()), 10)); err != nil {
		internalError(w, "session/ttl-store", err)
		return
	}
	d.Auth.SetSessionTTL(d2)
	writeData(w, http.StatusOK, sessionTTLResponse{
		TTLSeconds: int64(d2.Seconds()),
		Source:     "admin",
	}, nil)
}
