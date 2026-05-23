package api

import (
	"net/http"
	"strconv"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/models"
)

type createShareReq struct {
	ArticleID int64  `json:"article_id"`
	ToUser    int64  `json:"to_user"`
	Note      string `json:"note"`
}

func (d *Dependencies) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req createShareReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ArticleID == 0 || req.ToUser == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "article_id and to_user required")
		return
	}
	if req.ToUser == u.ID {
		writeError(w, http.StatusBadRequest, "bad_request", "cannot share to yourself")
		return
	}
	sh, err := d.Store.CreateShare(r.Context(), models.Share{
		ArticleID: req.ArticleID, FromUser: u.ID, ToUser: req.ToUser, Note: req.Note,
	})
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusCreated, sh, nil)
}

func (d *Dependencies) handleListInbox(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	unseen := q.Get("unseen") == "1"
	shares, err := d.Store.Inbox(r.Context(), u.ID, unseen, limit)
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, shares, nil)
}

func (d *Dependencies) handleMarkShareSeen(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	if mapStoreError(w, d.Store.MarkShareSeen(r.Context(), u.ID, id)) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}
