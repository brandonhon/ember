package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

type addFeedReq struct {
	URL        string `json:"url"`
	CategoryID *int64 `json:"category_id,omitempty"`
}

type updateFeedReq struct {
	TitleOverride *string `json:"title_override,omitempty"`
	CategoryID    *int64  `json:"category_id,omitempty"`
	ClearCategory bool    `json:"clear_category,omitempty"`
	Muted         *bool   `json:"muted,omitempty"`
}

func (d *Dependencies) handleListFeeds(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	feeds, err := d.Store.ListFeedsForUser(r.Context(), u.ID)
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, feeds, nil)
}

func (d *Dependencies) handleAddFeed(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req addFeedReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "url required")
		return
	}
	f, err := d.Store.UpsertFeed(r.Context(), models.Feed{URL: req.URL, Title: req.URL})
	if mapStoreError(w, err) {
		return
	}
	sub, err := d.Store.Subscribe(r.Context(), models.Subscription{
		UserID: u.ID, FeedID: f.ID, CategoryID: req.CategoryID,
	})
	if mapStoreError(w, err) {
		return
	}
	// Initial refresh: do it inline (cheap with mocked poller in tests; real
	// poller will fire fetch+parse synchronously, which is fine for a single
	// feed — caller is already paying a network cost). We use the request
	// context's parent (a Background ctx) to survive the handler return.
	if d.Poller != nil {
		_ = d.Poller.RefreshFeed(context.Background(), f.ID)
	}
	writeData(w, http.StatusCreated, map[string]any{"feed": f, "subscription": sub}, nil)
}

func (d *Dependencies) handleUpdateFeed(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	var req updateFeedReq
	if !decodeJSON(w, r, &req) {
		return
	}
	patch := store.UpdateSubscriptionPatch{
		TitleOverride: req.TitleOverride,
		CategoryID:    req.CategoryID,
		ClearCategory: req.ClearCategory,
		Muted:         req.Muted,
	}
	if mapStoreError(w, d.Store.UpdateSubscription(r.Context(), u.ID, id, patch)) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}

func (d *Dependencies) handleDeleteFeed(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	if mapStoreError(w, d.Store.Unsubscribe(r.Context(), u.ID, id)) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}

func (d *Dependencies) handleRefreshFeed(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	// Resolve subscription id → feed id. Reject cross-user.
	sub, err := d.Store.GetSubscriptionByID(r.Context(), u.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "feed not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if d.Poller != nil {
		if err := d.Poller.RefreshFeed(r.Context(), sub.FeedID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}

func (d *Dependencies) handleOPMLImport(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "multipart file 'file' required")
		return
	}
	defer file.Close()

	n, err := d.OPML.Import(r.Context(), u.ID, file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeData(w, http.StatusOK, map[string]int{"imported": n}, nil)
}

func (d *Dependencies) handleOPMLExport(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var buf bytes.Buffer
	if err := d.OPML.Export(r.Context(), u.ID, &buf); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/x-opml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="ember.opml"`)
	_, _ = w.Write(buf.Bytes())
}
