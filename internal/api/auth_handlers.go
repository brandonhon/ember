package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/store"
)

const maxSettingsJSON = 64 << 10 // 64 KiB

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (d *Dependencies) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "username and password required")
		return
	}
	u, err := d.Auth.Login(r.Context(), w, r, req.Username, req.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
		return
	}
	if err != nil {
		internalError(w, "internal", err)
		return
	}
	// Return an explicit, minimal shape rather than the raw models.User — the
	// SPA discards this body and re-pulls /api/me anyway, and enumerating the
	// fields means a future sensitive field on User can't silently leak out of
	// the login endpoint.
	writeData(w, http.StatusOK, loginResponse{
		ID: u.ID, Username: u.Username, IsAdmin: u.IsAdmin, CreatedAt: u.CreatedAt,
	}, nil)
}

// loginResponse is the login endpoint's allowlisted view of the user.
type loginResponse struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	IsAdmin   bool   `json:"is_admin"`
	CreatedAt int64  `json:"created_at"`
}

func (d *Dependencies) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := d.Auth.DestroySession(r.Context(), w, r); err != nil {
		internalError(w, "internal", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]bool{"ok": true}})
}

// meResponse extends the user record with derived fields the SPA needs (Fever
// api_key for mobile clients, etc).
type meResponse struct {
	User        any    `json:"user"`
	FeverAPIKey string `json:"fever_api_key"`
	Version     string `json:"version"`
	// FreshWindowSeconds lets the SPA's isFresh() use the same cutoff the
	// server uses for the Fresh badge + Fresh list. Zero/missing on the
	// client falls back to 6h to match the server's own fallback.
	FreshWindowSeconds int64 `json:"fresh_window_seconds"`
	// SummariesEnabled tells the SPA whether AI summarization is wired up
	// on this server. False when EMBER_DISABLE_SUMMARIES=1 or no Ollama
	// summarizer is configured (e.g. test mode). The Sidebar uses this to
	// hide the per-feed "Resummarize" action that would otherwise enqueue
	// work for a worker pool that isn't running.
	SummariesEnabled bool `json:"summaries_enabled"`
}

// Version is populated by main.go at startup so /api/me can surface it.
var Version = "dev"

func (d *Dependencies) handleMe(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "no user")
		return
	}
	// Lazily backfill a random Fever API token if this user doesn't have
	// one. Old users predate the column; new users without the migration
	// applied also land here.
	if u.FeverToken == "" {
		token, err := randomToken()
		if err != nil {
			internalError(w, "me/fever-token", err)
			return
		}
		if err := d.Store.SetFeverToken(r.Context(), u.ID, token); err != nil {
			internalError(w, "me/fever-token-store", err)
			return
		}
		u.FeverToken = token
	}
	fw := d.FreshWindow
	if fw <= 0 {
		fw = 6 * time.Hour
	}
	resp := meResponse{
		User:               u,
		FeverAPIKey:        u.FeverToken,
		Version:            Version,
		FreshWindowSeconds: int64(fw.Seconds()),
		SummariesEnabled:   d.Ollama != nil,
	}
	writeData(w, http.StatusOK, resp, nil)
}

// randomToken returns 32 cryptographically random bytes hex-encoded (64 chars).
func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// PasswordChangeReq carries the old + new password for a self-service password
// change.
type passwordChangeReq struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// handleChangePassword lets a user update their own password. Requires the
// current password to be supplied so a stolen session can't quietly take over
// the account.
func (d *Dependencies) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req passwordChangeReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "new_password required")
		return
	}
	if len(req.NewPassword) < 8 {
		writeError(w, http.StatusBadRequest, "weak_password", "new password must be at least 8 characters")
		return
	}
	if err := d.Auth.VerifyPassword(req.OldPassword, u.PasswordHash); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "current password is wrong")
		return
	}
	hash, err := d.Auth.HashPassword(req.NewPassword)
	if err != nil {
		internalError(w, "internal", err)
		return
	}
	if mapStoreError(w, d.Store.UpdateUser(r.Context(), u.ID, store.UpdateUserPatch{PasswordHash: &hash})) {
		return
	}
	// Invalidate every existing session for this user. Re-issue one for the
	// current browser so the user stays logged in here.
	if err := d.Auth.DeleteUserSessions(r.Context(), u.ID); err != nil {
		internalError(w, "password-change/delete-sessions", err)
		return
	}
	if _, err := d.Auth.CreateSession(r.Context(), w, r, u.ID); err != nil {
		internalError(w, "password-change/recreate-session", err)
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}

type updateSettingsReq struct {
	SettingsJSON string `json:"settings_json"`
}

func (d *Dependencies) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req updateSettingsReq
	if !decodeJSON(w, r, &req) {
		return
	}
	settings := req.SettingsJSON
	if settings == "" {
		settings = "{}"
	}
	if len(settings) > maxSettingsJSON {
		writeError(w, http.StatusBadRequest, "bad_request", "settings too large (max 64 KiB)")
		return
	}
	if !json.Valid([]byte(settings)) {
		writeError(w, http.StatusBadRequest, "bad_request", "settings must be valid JSON")
		return
	}
	if mapStoreError(w, d.Store.UpdateUser(r.Context(), u.ID, store.UpdateUserPatch{SettingsJSON: &settings})) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}

type updateEmailReq struct {
	Email           string `json:"email"`
	CurrentPassword string `json:"current_password"`
}

// handleUpdateEmail lets a signed-in user set or clear their own profile email
// (used for the daily digest + account contact). Empty clears it; a non-empty
// value must parse as a single RFC 5322 address. Self-service only — the admin
// Users section can still set anyone's email.
func (d *Dependencies) handleUpdateEmail(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req updateEmailReq
	if !decodeJSON(w, r, &req) {
		return
	}
	// Re-authenticate with the current password: a stolen session shouldn't be
	// able to silently redirect the account's digest email. Mirrors the
	// password-change requirement.
	if err := d.Auth.VerifyPassword(req.CurrentPassword, u.PasswordHash); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "current password is wrong")
		return
	}
	email := strings.TrimSpace(req.Email)
	if len(email) > 254 {
		writeError(w, http.StatusBadRequest, "bad_request", "email too long")
		return
	}
	if email != "" {
		if _, err := mail.ParseAddress(email); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "not a valid email address")
			return
		}
	}
	err := d.Store.UpdateUser(r.Context(), u.ID, store.UpdateUserPatch{Email: &email})
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "conflict", "that email address is already in use")
		return
	}
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, map[string]string{"email": email}, nil)
}
