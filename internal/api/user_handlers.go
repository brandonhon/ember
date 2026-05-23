package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

func (d *Dependencies) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := d.Store.ListUsers(r.Context())
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, users, nil)
}

type createUserReq struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	IsAdmin  bool   `json:"is_admin"`
}

func (d *Dependencies) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "username and password required")
		return
	}
	hash, err := d.Auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	u, err := d.Store.CreateUser(r.Context(), models.User{
		Username: req.Username, Email: req.Email,
		PasswordHash: hash, IsAdmin: req.IsAdmin,
	})
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusCreated, u, nil)
}

type updateUserReq struct {
	Email    *string `json:"email,omitempty"`
	Password *string `json:"password,omitempty"`
	IsAdmin  *bool   `json:"is_admin,omitempty"`
}

func (d *Dependencies) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	var req updateUserReq
	if !decodeJSON(w, r, &req) {
		return
	}
	patch := store.UpdateUserPatch{Email: req.Email, IsAdmin: req.IsAdmin}
	if req.Password != nil {
		hash, err := d.Auth.HashPassword(*req.Password)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		patch.PasswordHash = &hash
	}
	if mapStoreError(w, d.Store.UpdateUser(r.Context(), id, patch)) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}

func (d *Dependencies) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	// Don't allow admin to delete themselves.
	if cur, _ := auth.FromContext(r.Context()); cur.ID == id {
		writeError(w, http.StatusBadRequest, "bad_request", "cannot delete yourself")
		return
	}
	if mapStoreError(w, d.Store.DeleteUser(r.Context(), id)) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}

func paramInt(w http.ResponseWriter, r *http.Request, key string) (int64, bool) {
	v := chi.URLParam(r, key)
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid "+key)
		return 0, false
	}
	return id, true
}
