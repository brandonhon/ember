package api

import (
	"net/http"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/filters"
	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

type filterReq struct {
	Name      string `json:"name"`
	MatchJSON string `json:"match_json"`
	Action    string `json:"action"`
	Enabled   *bool  `json:"enabled,omitempty"`
}

func (d *Dependencies) handleListFilters(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	fs, err := d.Store.ListFilters(r.Context(), u.ID)
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, fs, nil)
}

func (d *Dependencies) handleCreateFilter(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req filterReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "name required")
		return
	}
	// Validate match + action up front so bad data never lands in the DB.
	if _, err := filters.ParseMatch(req.MatchJSON); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := filters.ValidateAction(req.Action); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	f, err := d.Store.CreateFilter(r.Context(), models.Filter{
		UserID: u.ID, Name: req.Name, MatchJSON: req.MatchJSON,
		Action: req.Action, Enabled: enabled,
	})
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusCreated, f, nil)
}

func (d *Dependencies) handleUpdateFilter(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	var req filterReq
	if !decodeJSON(w, r, &req) {
		return
	}
	patch := store.UpdateFilterPatch{}
	if req.Name != "" {
		patch.Name = &req.Name
	}
	if req.MatchJSON != "" {
		if _, err := filters.ParseMatch(req.MatchJSON); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		patch.MatchJSON = &req.MatchJSON
	}
	if req.Action != "" {
		if err := filters.ValidateAction(req.Action); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		patch.Action = &req.Action
	}
	if req.Enabled != nil {
		patch.Enabled = req.Enabled
	}
	if mapStoreError(w, d.Store.UpdateFilter(r.Context(), u.ID, id, patch)) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}

func (d *Dependencies) handleDeleteFilter(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	if mapStoreError(w, d.Store.DeleteFilter(r.Context(), u.ID, id)) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}
