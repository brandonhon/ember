package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// userMini is the public projection of a user shown to non-admin callers
// (the share-modal user picker, mainly). Hides email, is_admin,
// settings_json, and created_at.
type userMini struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// adminUserView is the admin-list projection. It deliberately omits
// SettingsJSON — a user's private preference blob is no business of another
// account, even an admin's. The owning user reads their own settings via
// /api/me.
type adminUserView struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Email     string `json:"email,omitempty"`
	IsAdmin   bool   `json:"is_admin"`
	CreatedAt int64  `json:"created_at"`
}

func (d *Dependencies) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := d.Store.ListUsersPublic(r.Context())
	if mapStoreError(w, err) {
		return
	}
	caller, _ := auth.FromContext(r.Context())
	if caller.IsAdmin {
		out := make([]adminUserView, 0, len(users))
		for _, u := range users {
			out = append(out, adminUserView{
				ID: u.ID, Username: u.Username, Email: u.Email,
				IsAdmin: u.IsAdmin, CreatedAt: u.CreatedAt,
			})
		}
		writeData(w, http.StatusOK, out, nil)
		return
	}
	// Non-admin caller: minimal projection only.
	out := make([]userMini, 0, len(users))
	for _, u := range users {
		out = append(out, userMini{ID: u.ID, Username: u.Username})
	}
	writeData(w, http.StatusOK, out, nil)
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
	// Match the 8-char minimum enforced on the change-password path so the admin
	// create-user route can't seed a zero-entropy account.
	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "weak_password", "password must be at least 8 characters")
		return
	}
	hash, err := d.Auth.HashPassword(req.Password)
	if err != nil {
		internalError(w, "internal", err)
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
	caller, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	var req updateUserReq
	if !decodeJSON(w, r, &req) {
		return
	}
	// Guard against admins removing their own admin role — permanent lockout
	// if they are the sole admin.
	if req.IsAdmin != nil && !*req.IsAdmin && id == caller.ID {
		writeError(w, http.StatusBadRequest, "bad_request", "you cannot remove your own admin role")
		return
	}
	patch := store.UpdateUserPatch{Email: req.Email, IsAdmin: req.IsAdmin}
	if req.Password != nil {
		// Enforce the same 8-character minimum that create-user uses.
		if len(*req.Password) < 8 {
			writeError(w, http.StatusBadRequest, "bad_request", "password must be at least 8 characters")
			return
		}
		hash, err := d.Auth.HashPassword(*req.Password)
		if err != nil {
			internalError(w, "internal", err)
			return
		}
		patch.PasswordHash = &hash
	}
	if mapStoreError(w, d.Store.UpdateUser(r.Context(), id, patch)) {
		return
	}
	// If the admin changed this user's password, invalidate any sessions
	// they have open. The admin's own session is unaffected (different
	// user_id).
	if req.Password != nil {
		if err := d.Auth.DeleteUserSessions(r.Context(), id); err != nil {
			internalError(w, "admin-password-change/delete-sessions", err)
			return
		}
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
