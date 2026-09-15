package api

import (
	"log/slog"
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
	writeOK(w)
}

type updateBoardReq struct {
	Summarize *bool `json:"summarize,omitempty"`
}

// handleUpdateBoard toggles whether pinning an article to this board asks for
// a summary under on-demand mode (issue #199) — a board used for filing rather
// than reading should not spend inference just because something landed in it.
//
// The board is resolved for the caller FIRST, the same way handleMarkAllRead
// guards its board scope: a foreign or unknown id has to be a 404 before any
// write is attempted, not a silent success. UpdateBoard is user-scoped too, so
// this is belt and braces — but it is also what makes the 404 truthful instead
// of an ErrNotFound that could equally mean "no rows changed".
func (d *Dependencies) handleUpdateBoard(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	b, err := d.Store.GetBoard(r.Context(), u.ID, id)
	if mapStoreError(w, err) {
		return
	}
	var req updateBoardReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Summarize == nil {
		// Nothing asked for: echo the board rather than writing a value the
		// caller never sent. Matches the settings patch's pointer-bag rule.
		writeData(w, http.StatusOK, b, nil)
		return
	}
	if mapStoreError(w, d.Store.UpdateBoard(r.Context(), u.ID, id, *req.Summarize)) {
		return
	}
	b.Summarize = *req.Summarize
	writeData(w, http.StatusOK, b, nil)
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
	// Pinning to a board is the third "I mean to read this" signal, unless the
	// board is one the user files things in rather than reads from — hence the
	// per-board flag. User-scoped via BoardSummarizes, matching every other
	// board lookup: a foreign board id must not leak whether it exists, let
	// alone trigger inference on someone else's behalf.
	if wants, err := d.Store.BoardSummarizes(r.Context(), u.ID, boardID); err != nil {
		slog.Default().Warn("boards: summarize flag lookup", "board_id", boardID, "err", err)
	} else if wants {
		if changed, err := d.Store.RequestSummary(r.Context(), req.ArticleID); err != nil {
			slog.Default().Warn("boards: request summary", "article_id", req.ArticleID, "err", err)
		} else if changed {
			d.enqueueSummaries([]int64{req.ArticleID})
		}
	}
	writeOK(w)
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
	writeOK(w)
}
