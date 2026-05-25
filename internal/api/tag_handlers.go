package api

import (
	"net/http"

	"github.com/brandonhon/ember/internal/auth"
)

func (d *Dependencies) handleListArticleTags(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	tags, err := d.Store.ListArticleTags(r.Context(), u.ID, id)
	if mapStoreError(w, err) {
		return
	}
	if tags == nil {
		tags = []string{}
	}
	writeData(w, http.StatusOK, tags, nil)
}

type tagReq struct {
	Tag string `json:"tag"`
}

func (d *Dependencies) handleAddArticleTag(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	var req tagReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Tag == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "tag required")
		return
	}
	if err := d.Store.AddArticleTag(r.Context(), u.ID, id, req.Tag); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	tags, _ := d.Store.ListArticleTags(r.Context(), u.ID, id)
	writeData(w, http.StatusOK, tags, nil)
}

func (d *Dependencies) handleRemoveArticleTag(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	tag := r.URL.Query().Get("tag")
	if tag == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "tag query param required")
		return
	}
	if err := d.Store.RemoveArticleTag(r.Context(), u.ID, id, tag); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	tags, _ := d.Store.ListArticleTags(r.Context(), u.ID, id)
	writeData(w, http.StatusOK, tags, nil)
}

// handleListUserTags returns every tag the user has used, with counts.
func (d *Dependencies) handleListUserTags(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	tags, err := d.Store.ListUserTags(r.Context(), u.ID)
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, tags, nil)
}
