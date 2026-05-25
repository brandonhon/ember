package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

func (d *Dependencies) handleListSavedSearches(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	out, err := d.Store.ListSavedSearches(r.Context(), u.ID)
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, out, nil)
}

type savedSearchReq struct {
	Name  string `json:"name"`
	Query string `json:"query"`
}

func (d *Dependencies) handleCreateSavedSearch(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req savedSearchReq
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Query = strings.TrimSpace(req.Query)
	if req.Name == "" || req.Query == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "name and query required")
		return
	}
	ss, err := d.Store.CreateSavedSearch(r.Context(), models.SavedSearch{
		UserID: u.ID, Name: req.Name, Query: req.Query,
	})
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "conflict", "a saved search with that name already exists")
		return
	}
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusCreated, ss, nil)
}

func (d *Dependencies) handleDeleteSavedSearch(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	if mapStoreError(w, d.Store.DeleteSavedSearch(r.Context(), u.ID, id)) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}
