package api

import (
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
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeData(w, http.StatusOK, u, nil)
}

func (d *Dependencies) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := d.Auth.DestroySession(r.Context(), w, r); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]bool{"ok": true}})
}

func (d *Dependencies) handleMe(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "no user")
		return
	}
	writeData(w, http.StatusOK, u, nil)
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
