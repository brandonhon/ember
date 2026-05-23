package api

import (
	"net/http"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/models"
)

func (d *Dependencies) handleListBoards(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	bs, err := d.Store.ListBoards(r.Context(), u.ID)
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, bs, nil)
}

type boardReq struct {
	Name string `json:"name"`
}

func (d *Dependencies) handleCreateBoard(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req boardReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "name required")
		return
	}
	b, err := d.Store.CreateBoard(r.Context(), models.Board{UserID: u.ID, Name: req.Name})
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusCreated, b, nil)
}

func (d *Dependencies) handleDeleteBoard(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	if mapStoreError(w, d.Store.DeleteBoard(r.Context(), u.ID, id)) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}

type boardAddReq struct {
	ArticleID int64 `json:"article_id"`
}

func (d *Dependencies) handleBoardAdd(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	boardID, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	var req boardAddReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if mapStoreError(w, d.Store.AddArticleToBoard(r.Context(), u.ID, boardID, req.ArticleID)) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}

func (d *Dependencies) handleBoardRemove(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	boardID, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	articleID, ok := paramInt(w, r, "articleId")
	if !ok {
		return
	}
	if mapStoreError(w, d.Store.RemoveArticleFromBoard(r.Context(), u.ID, boardID, articleID)) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}
