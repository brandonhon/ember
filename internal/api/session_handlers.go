package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/brandonhon/ember/internal/auth"
)

// keySessionTTL is the app_settings key that persists an admin-configured
// session TTL across restarts. Value is seconds, stored as decimal string.
// Empty (or unset) means "fall through to the env-var / DefaultSessionTTL
// that auth.New picked at boot".
const keySessionTTL = "session_ttl_seconds"

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
	source := "default"
	if v, _ := d.Store.GetAppSetting(r.Context(), keySessionTTL); v != "" {
		source = "admin"
	}
	// Read through Auth's lock — direct field access would race the admin
	// handler below if two admins were configuring simultaneously.
	ttl := d.Auth.EffectiveSessionTTL()
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
// Admin-only. Range-validated via auth.SetSessionTTL which is the single
// source of truth for the bounds (auth.MinSessionTTL, auth.MaxSessionTTL).
func (d *Dependencies) handleSetSessionTTL(w http.ResponseWriter, r *http.Request) {
	var req setSessionTTLReq
	if !decodeJSON(w, r, &req) {
		return
	}
	d2 := time.Duration(req.TTLSeconds) * time.Second
	// Pre-check + early 400 with a clear message (auth.SetSessionTTL returns
	// the same error but we want to surface the bounds in the response body).
	if d2 < auth.MinSessionTTL || d2 > auth.MaxSessionTTL {
		writeError(w, http.StatusBadRequest, "bad_request",
			"ttl_seconds must be between "+strconv.FormatInt(int64(auth.MinSessionTTL.Seconds()), 10)+
				" and "+strconv.FormatInt(int64(auth.MaxSessionTTL.Seconds()), 10))
		return
	}
	if err := d.Store.PutAppSetting(r.Context(), keySessionTTL,
		strconv.FormatInt(int64(d2.Seconds()), 10)); err != nil {
		internalError(w, "session/ttl-store", err)
		return
	}
	if err := d.Auth.SetSessionTTL(d2); err != nil {
		// Should be unreachable given the pre-check above, but if the bounds
		// ever drift between api and auth packages, the auth check is the
		// final gate.
		if errors.Is(err, auth.ErrSessionTTLOutOfRange) {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		internalError(w, "session/ttl-apply", err)
		return
	}
	writeData(w, http.StatusOK, sessionTTLResponse{
		TTLSeconds: int64(d2.Seconds()),
		Source:     "admin",
	}, nil)
}
