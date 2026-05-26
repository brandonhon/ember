package api

import (
	"errors"
	"net/http"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/store"
)

// requireArticleAccess returns (articleID, true) when the calling user is
// subscribed to the article's feed. Otherwise writes 404 and returns false.
// Used by every per-article endpoint to enforce ownership instead of letting
// users enumerate article ids across the global articles table.
func (d *Dependencies) requireArticleAccess(w http.ResponseWriter, r *http.Request, userID int64) (int64, bool) {
	id, ok := paramInt(w, r, "id")
	if !ok {
		return 0, false
	}
	if _, err := d.Store.GetArticleForUser(r.Context(), userID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "article not found")
			return 0, false
		}
		internalError(w, "tag/access", err)
		return 0, false
	}
	return id, true
}

func (d *Dependencies) handleListArticleTags(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := d.requireArticleAccess(w, r, u.ID)
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
	id, ok := d.requireArticleAccess(w, r, u.ID)
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
		internalError(w, "internal", err)
		return
	}
	tags, _ := d.Store.ListArticleTags(r.Context(), u.ID, id)
	writeData(w, http.StatusOK, tags, nil)
}

func (d *Dependencies) handleRemoveArticleTag(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := d.requireArticleAccess(w, r, u.ID)
	if !ok {
		return
	}
	tag := r.URL.Query().Get("tag")
	if tag == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "tag query param required")
		return
	}
	if err := d.Store.RemoveArticleTag(r.Context(), u.ID, id, tag); err != nil {
		internalError(w, "internal", err)
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
