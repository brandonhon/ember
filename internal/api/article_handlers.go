package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/store"
)

func (d *Dependencies) handleListArticles(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	q := r.URL.Query()

	view := q.Get("view")

	atoi := func(key string) int64 {
		v, _ := strconv.ParseInt(q.Get(key), 10, 64)
		return v
	}
	atoB := func(key string) bool {
		v := q.Get(key)
		return v == "1" || v == "true"
	}

	freshAfter := atoi("fresh_after")
	if view == "fresh" && freshAfter == 0 {
		// default: 6 hours.
		freshAfter = time.Now().Add(-6 * time.Hour).Unix()
	}
	if view == "today" && freshAfter == 0 {
		now := time.Now()
		dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		freshAfter = dayStart.Unix()
	}

	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}

	query := store.ListArticlesQuery{
		View:            view,
		FeedID:          atoi("feed_id"),
		CategoryID:      atoi("category_id"),
		BoardID:         atoi("board_id"),
		Unread:          atoB("unread"),
		Starred:         atoB("starred"),
		Later:           atoB("later"),
		FreshAfter:      freshAfter,
		Limit:           limit,
		PublishedBefore: atoi("cursor_pub"),
		IDBefore:        atoi("cursor_id"),
		// Hide articles the LLM hasn't touched yet — they appear once the
		// poller stamps a summary_model (success or 'skipped'). Admin/debug
		// callers can pass ?all=1 to bypass.
		OnlySummarized: !atoB("all"),
		Tag:            q.Get("tag"),
	}
	articles, err := d.Store.ListArticles(r.Context(), u.ID, query)
	if mapStoreError(w, err) {
		return
	}
	meta := map[string]any{}
	if n := len(articles); n > 0 {
		last := articles[n-1]
		meta["next_cursor_pub"] = last.PublishedAt
		meta["next_cursor_id"] = last.ID
	}
	writeData(w, http.StatusOK, articles, meta)
}

func (d *Dependencies) handleGetArticle(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	a, err := d.Store.GetArticleForUser(r.Context(), u.ID, id)
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, a, nil)
}

type setReadReq struct {
	IDs  []int64 `json:"ids"`
	Read bool    `json:"read"`
}

func (d *Dependencies) handleSetRead(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req setReadReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if mapStoreError(w, d.Store.SetRead(r.Context(), u.ID, req.IDs, req.Read)) {
		return
	}
	writeData(w, http.StatusOK, map[string]int{"count": len(req.IDs)}, nil)
}

type setFlagReq struct {
	ID    int64 `json:"id"`
	Value bool  `json:"value"`
}

func (d *Dependencies) handleSetStar(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req setFlagReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if mapStoreError(w, d.Store.SetStarred(r.Context(), u.ID, req.ID, req.Value)) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}

func (d *Dependencies) handleSetLater(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req setFlagReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if mapStoreError(w, d.Store.SetLater(r.Context(), u.ID, req.ID, req.Value)) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}

type markAllReadReq struct {
	FeedID     int64  `json:"feed_id,omitempty"`
	CategoryID int64  `json:"category_id,omitempty"`
	BoardID    int64  `json:"board_id,omitempty"`
	View       string `json:"view,omitempty"`
}

func (d *Dependencies) handleMarkAllRead(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req markAllReadReq
	if !decodeJSON(w, r, &req) {
		return
	}
	// Boards take a different path — board_articles join, not subscription scope.
	if req.BoardID > 0 {
		n, err := d.Store.MarkBoardRead(r.Context(), u.ID, req.BoardID)
		if mapStoreError(w, err) {
			return
		}
		writeData(w, http.StatusOK, map[string]int64{"count": n}, nil)
		return
	}
	var freshAfter int64
	switch req.View {
	case "fresh":
		freshAfter = time.Now().Add(-6 * time.Hour).Unix()
	case "today":
		now := time.Now()
		freshAfter = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	}
	n, err := d.Store.MarkAllRead(r.Context(), u.ID, req.FeedID, req.CategoryID, freshAfter)
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, map[string]int64{"count": n}, nil)
}
