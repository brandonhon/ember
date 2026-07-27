package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/store"
)

// passkeySummary is the user-facing view of a registered credential. Raw
// public-key material is never exposed.
type passkeySummary struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt int64  `json:"last_used_at"`
}

// handlePasskeyExists is a PUBLIC endpoint (no auth) returning a single
// boolean: does this server have at least one passkey registered? Drives
// the login UI gate so the "Sign in with passkey" button only renders when
// trying a passkey could plausibly succeed. Intentionally NOT per-username
// to avoid an enumeration oracle. WebAuthn must also be configured (i.e.
// EMBER_PUBLIC_URL is set) — otherwise even an existing passkey can't be
// used to sign in.
func (d *Dependencies) handlePasskeyExists(w http.ResponseWriter, r *http.Request) {
	if d.WebAuthn == nil {
		writeData(w, http.StatusOK, map[string]bool{"any_registered": false}, nil)
		return
	}
	exists, err := d.Store.AnyPasskeyExists(r.Context())
	if err != nil {
		internalError(w, "passkeys/exists", err)
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"any_registered": exists}, nil)
}

func (d *Dependencies) handleListPasskeys(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	pks, err := d.Store.ListPasskeys(r.Context(), u.ID)
	if err != nil {
		internalError(w, "passkeys/list", err)
		return
	}
	out := make([]passkeySummary, 0, len(pks))
	for _, p := range pks {
		out = append(out, passkeySummary{
			ID:         p.ID,
			Name:       p.Name,
			CreatedAt:  p.CreatedAt,
			LastUsedAt: p.LastUsedAt,
		})
	}
	writeData(w, http.StatusOK, out, nil)
}

// passkeyCeremonyResp returns the WebAuthn options plus a session ID the client
// must echo back on finish. Shared by the register and login begin endpoints —
// both ceremonies have the same wire shape.
type passkeyCeremonyResp struct {
	SessionID string          `json:"session_id"`
	Options   json.RawMessage `json:"options"`
}

// passkeyFinishReq carries a completed ceremony back from the browser. Register
// also sets Name (a friendly label for the new credential); login ignores it.
type passkeyFinishReq struct {
	SessionID string          `json:"session_id"`
	Name      string          `json:"name,omitempty"`
	Response  json.RawMessage `json:"response"`
}

// passkeyID parses the {id} path param. Writes the 400 and returns ok=false on
// a malformed value.
func passkeyID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_id", "invalid passkey id")
		return 0, false
	}
	return id, true
}

func (d *Dependencies) handlePasskeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if d.WebAuthn == nil {
		writeError(w, http.StatusServiceUnavailable, "webauthn_disabled", "passkeys are not configured (set EMBER_PUBLIC_URL)")
		return
	}
	u, _ := auth.FromContext(r.Context())
	opts, sid, err := d.WebAuthn.BeginRegister(r.Context(), u)
	if err != nil {
		internalError(w, "passkeys/begin-register", err)
		return
	}
	writeData(w, http.StatusOK, passkeyCeremonyResp{SessionID: sid, Options: opts}, nil)
}

func (d *Dependencies) handlePasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if d.WebAuthn == nil {
		writeError(w, http.StatusServiceUnavailable, "webauthn_disabled", "passkeys are not configured")
		return
	}
	u, _ := auth.FromContext(r.Context())
	var req passkeyFinishReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.SessionID == "" || len(req.Response) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "session_id and response required")
		return
	}
	pk, err := d.WebAuthn.FinishRegister(r.Context(), req.SessionID, req.Name, req.Response, u.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "session_expired", "registration ceremony expired")
		return
	}
	if err != nil {
		// The go-webauthn error text leaks internals (CBOR/attestation/origin
		// detail). Log it; return a generic message.
		slog.Default().Warn("passkey registration failed", "err", err)
		writeError(w, http.StatusBadRequest, "registration_failed", "passkey registration failed")
		return
	}
	writeData(w, http.StatusOK, passkeySummary{
		ID: pk.ID, Name: pk.Name, CreatedAt: pk.CreatedAt,
	}, nil)
}

type passkeyRenameReq struct {
	Name string `json:"name"`
}

func (d *Dependencies) handlePasskeyRename(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := passkeyID(w, r)
	if !ok {
		return
	}
	var req passkeyRenameReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "name required")
		return
	}
	if mapStoreError(w, d.Store.RenamePasskey(r.Context(), u.ID, id, req.Name)) {
		return
	}
	writeOK(w)
}

func (d *Dependencies) handlePasskeyDelete(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := passkeyID(w, r)
	if !ok {
		return
	}
	if mapStoreError(w, d.Store.DeletePasskey(r.Context(), u.ID, id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Passkey login ----------------------------------------------------------

type passkeyLoginBeginReq struct {
	Username string `json:"username"`
}

func (d *Dependencies) handlePasskeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	if d.WebAuthn == nil {
		writeError(w, http.StatusServiceUnavailable, "webauthn_disabled", "passkeys are not configured")
		return
	}
	var req passkeyLoginBeginReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Username == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "username required")
		return
	}
	u, err := d.Store.GetUserByUsername(r.Context(), req.Username)
	if errors.Is(err, store.ErrNotFound) {
		// Equalize work with the found-user path (which also runs a passkey
		// lookup inside BeginLogin) so response timing doesn't reveal whether
		// the username exists, then return the same generic error.
		_, _ = d.Store.ListPasskeys(r.Context(), 0)
		writeError(w, http.StatusUnauthorized, "no_passkey", "this account has no passkey")
		return
	}
	if err != nil {
		internalError(w, "passkeys/login-begin/lookup", err)
		return
	}
	// Resolved per ceremony rather than cached at boot, so flipping the admin
	// toggle takes effect on the next sign-in without a restart.
	requireUV := d.Store.ResolvePasskeyRequireUV(r.Context(), d.PasskeyRequireUVFallback)
	opts, sid, err := d.WebAuthn.BeginLogin(r.Context(), u, requireUV)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "no_passkey", "this account has no passkey")
		return
	}
	writeData(w, http.StatusOK, passkeyCeremonyResp{SessionID: sid, Options: opts}, nil)
}

func (d *Dependencies) handlePasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	if d.WebAuthn == nil {
		writeError(w, http.StatusServiceUnavailable, "webauthn_disabled", "passkeys are not configured")
		return
	}
	var req passkeyFinishReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.SessionID == "" || len(req.Response) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "session_id and response required")
		return
	}
	u, err := d.WebAuthn.FinishLogin(r.Context(), req.SessionID, req.Response)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "session_expired", "passkey ceremony expired")
		return
	}
	if errors.Is(err, auth.ErrClonedAuthenticator) {
		// Worth its own log line at a louder level than a generic failure: this
		// means a credential's counter went backwards, which is either a
		// duplicated authenticator or a replayed assertion. The client still
		// gets the same opaque message.
		slog.Default().Error("passkey assertion rejected: signature counter did not advance",
			"ip", remoteIP(r, d.trustedNets))
		writeError(w, http.StatusUnauthorized, "passkey_failed", "passkey authentication failed")
		return
	}
	if err != nil {
		// Don't leak go-webauthn internals (sign-count/assertion detail) to the
		// client; log and return a generic failure.
		slog.Default().Warn("passkey assertion failed", "err", err,
			"ip", remoteIP(r, d.trustedNets))
		writeError(w, http.StatusUnauthorized, "passkey_failed", "passkey authentication failed")
		return
	}
	// Mirror password login: destroy any prior session cookie, then issue a
	// fresh one for the now-authenticated user.
	_ = d.Auth.DestroySession(r.Context(), w, r)
	if _, err := d.Auth.CreateSession(r.Context(), w, r, u.ID); err != nil {
		internalError(w, "passkeys/login-finish/session", err)
		return
	}
	// Same allowlisted shape as password login rather than the raw models.User,
	// so the two sign-in paths can't drift on what they expose.
	writeData(w, http.StatusOK, loginResponse{
		ID: u.ID, Username: u.Username, IsAdmin: u.IsAdmin, CreatedAt: u.CreatedAt,
	}, nil)
}
