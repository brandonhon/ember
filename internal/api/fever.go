package api

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// handleFever dispatches Fever API requests. Authentication is via the
// `api_key` form value, which is md5("username:password") — same as the spec.
// We compute that on demand for each user instead of storing it.
func (d *Dependencies) handleFever(w http.ResponseWriter, r *http.Request) {
	// Cap the form body — ParseForm reads it fully into memory. The Fever shim
	// only needs a handful of small fields; 64 KiB is generous. Without a
	// fronting proxy this is the only guard against a giant-body memory hog.
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"auth": 0, "api_version": 3})
		return
	}
	apiKey := r.FormValue("api_key")
	user, err := d.feverFindUser(r.Context(), apiKey)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"auth": 0, "api_version": 3})
		return
	}

	resp := map[string]any{
		"auth":                   1,
		"api_version":            3,
		"last_refreshed_on_time": d.Auth.Now().Unix(),
	}
	q := r.URL.Query()

	if _, ok := q["groups"]; ok {
		cats, _ := d.Store.ListCategories(r.Context(), user.ID)
		groups := make([]map[string]any, 0, len(cats))
		for _, c := range cats {
			groups = append(groups, map[string]any{"id": c.ID, "title": c.Name})
		}
		resp["groups"] = groups
		feeds, _ := d.Store.ListFeedsForUser(r.Context(), user.ID)
		fg := map[int64][]int64{}
		for _, f := range feeds {
			if f.CategoryID != nil {
				fg[*f.CategoryID] = append(fg[*f.CategoryID], f.ID)
			}
		}
		feedsGroups := make([]map[string]any, 0, len(fg))
		for catID, ids := range fg {
			strIds := make([]string, len(ids))
			for i, id := range ids {
				strIds[i] = strconv.FormatInt(id, 10)
			}
			feedsGroups = append(feedsGroups, map[string]any{
				"group_id": catID,
				"feed_ids": strings.Join(strIds, ","),
			})
		}
		resp["feeds_groups"] = feedsGroups
	}

	if _, ok := q["feeds"]; ok {
		feeds, _ := d.Store.ListFeedsForUser(r.Context(), user.ID)
		out := make([]map[string]any, 0, len(feeds))
		for _, f := range feeds {
			out = append(out, map[string]any{
				"id":                   f.ID,
				"favicon_id":           0,
				"title":                f.Title,
				"url":                  f.URL,
				"site_url":             f.SiteURL,
				"is_spark":             0,
				"last_updated_on_time": f.LastFetched,
			})
		}
		resp["feeds"] = out
	}

	if _, ok := q["unread_item_ids"]; ok {
		ids, _ := d.feverIDsForFlag(r.Context(), user.ID, "unread")
		resp["unread_item_ids"] = ids
	}
	if _, ok := q["saved_item_ids"]; ok {
		ids, _ := d.feverIDsForFlag(r.Context(), user.ID, "saved")
		resp["saved_item_ids"] = ids
	}

	if _, ok := q["items"]; ok {
		// Fever returns "items" — newest first, up to 50.
		articles, _ := d.Store.ListArticles(r.Context(), user.ID, store.ListArticlesQuery{Limit: 50})
		out := make([]map[string]any, 0, len(articles))
		for _, a := range articles {
			out = append(out, map[string]any{
				"id":              a.ID,
				"feed_id":         a.FeedID,
				"title":           a.Title,
				"author":          a.Author,
				"html":            a.ContentHTML,
				"url":             a.URL,
				"is_saved":        boolInt(a.IsStarred),
				"is_read":         boolInt(a.IsRead),
				"created_on_time": a.PublishedAt,
			})
		}
		resp["items"] = out
		resp["total_items"] = len(out)
	}

	if mark := r.FormValue("mark"); mark != "" {
		as := r.FormValue("as")
		idStr := r.FormValue("id")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		if mark == "item" && id > 0 {
			switch as {
			case "read":
				_ = d.Store.SetRead(r.Context(), user.ID, []int64{id}, true)
			case "unread":
				_ = d.Store.SetRead(r.Context(), user.ID, []int64{id}, false)
			case "saved":
				_ = d.Store.SetStarred(r.Context(), user.ID, id, true)
			case "unsaved":
				_ = d.Store.SetStarred(r.Context(), user.ID, id, false)
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (d *Dependencies) feverFindUser(ctx context.Context, apiKey string) (models.User, error) {
	if apiKey == "" {
		return models.User{}, store.ErrNotFound
	}
	users, err := d.Store.ListUsers(ctx)
	if err != nil {
		return models.User{}, err
	}
	// Constant-time compare per user so a network observer can't time-extract
	// the matching token. ListUsers is bounded and tiny in the homelab case.
	provided := []byte(apiKey)
	for _, u := range users {
		if u.FeverToken == "" {
			continue
		}
		if subtle.ConstantTimeCompare(provided, []byte(u.FeverToken)) == 1 {
			return u, nil
		}
	}
	return models.User{}, store.ErrNotFound
}

func (d *Dependencies) feverIDsForFlag(ctx context.Context, userID int64, flag string) (string, error) {
	q := store.ListArticlesQuery{Limit: 200}
	switch flag {
	case "unread":
		q.Unread = true
	case "saved":
		q.Starred = true
	}
	rows, err := d.Store.ListArticles(ctx, userID, q)
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(rows))
	for _, a := range rows {
		parts = append(parts, strconv.FormatInt(a.ID, 10))
	}
	return strings.Join(parts, ","), nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
