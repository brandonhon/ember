package api

import (
	"net/http"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

type categoryReq struct {
	Name     string `json:"name"`
	Color    string `json:"color"`
	Position *int   `json:"position,omitempty"`
}

func (d *Dependencies) handleListCategories(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	cats, err := d.Store.ListCategories(r.Context(), u.ID)
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, cats, nil)
}

func (d *Dependencies) handleCreateCategory(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req categoryReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "name required")
		return
	}
	pos := 0
	if req.Position != nil {
		pos = *req.Position
	}
	c, err := d.Store.CreateCategory(r.Context(), models.Category{
		UserID: u.ID, Name: req.Name, Color: req.Color, Position: pos,
	})
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusCreated, c, nil)
}

func (d *Dependencies) handleUpdateCategory(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	var req categoryReq
	if !decodeJSON(w, r, &req) {
		return
	}
	patch := store.UpdateCategoryPatch{}
	if req.Name != "" {
		patch.Name = &req.Name
	}
	if req.Color != "" {
		patch.Color = &req.Color
	}
	if req.Position != nil {
		patch.Position = req.Position
	}
	if mapStoreError(w, d.Store.UpdateCategory(r.Context(), u.ID, id, patch)) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}

type reorderReq struct {
	IDs []int64 `json:"ids"`
}

func (d *Dependencies) handleReorderCategories(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req reorderReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if mapStoreError(w, d.Store.ReorderCategories(r.Context(), u.ID, req.IDs)) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}

func (d *Dependencies) handleReorderFeeds(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req reorderReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if mapStoreError(w, d.Store.ReorderSubscriptions(r.Context(), u.ID, req.IDs)) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}

func (d *Dependencies) handleDeleteCategory(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	if mapStoreError(w, d.Store.DeleteCategory(r.Context(), u.ID, id)) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}
