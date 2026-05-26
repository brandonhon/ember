package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/store"
)

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
	writeData(w, http.StatusOK, u, nil)
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
	resp := meResponse{
		User:        u,
		FeverAPIKey: u.FeverToken,
		Version:     Version,
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
	if mapStoreError(w, d.Store.UpdateUser(r.Context(), u.ID, store.UpdateUserPatch{SettingsJSON: &settings})) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}
