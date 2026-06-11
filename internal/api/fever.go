package api

import (
	"context"
	"crypto/subtle"
	"log/slog"
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
		feeds, _ := d.Store.ListFeedsForUser(r.Context(), user.ID, 0, false)
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
		feeds, _ := d.Store.ListFeedsForUser(r.Context(), user.ID, 0, false)
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
		ids, _ := d.Store.FeverItemIDs(r.Context(), user.ID, "unread")
		resp["unread_item_ids"] = joinIDs(ids)
	}
	if _, ok := q["saved_item_ids"]; ok {
		ids, _ := d.Store.FeverItemIDs(r.Context(), user.ID, "saved")
		resp["saved_item_ids"] = joinIDs(ids)
	}

	if _, ok := q["items"]; ok {
		// Fever items are paged by id: since_id walks forward (the normal sync
		// path), max_id backfills, with_ids fetches an explicit set, and no
		// argument returns the most recent page. Non-deduped, so every id in
		// unread_item_ids is fetchable here.
		fq := store.FeverItemQuery{Limit: 50}
		if v := q.Get("since_id"); v != "" {
			fq.SinceID, _ = strconv.ParseInt(v, 10, 64)
		}
		if v := q.Get("max_id"); v != "" {
			fq.MaxID, _ = strconv.ParseInt(v, 10, 64)
		}
		if v := q.Get("with_ids"); v != "" {
			fq.WithIDs = parseFeverIDs(v)
		}
		articles, err := d.Store.FeverItems(r.Context(), user.ID, fq)
		if err != nil {
			// Fever clients expect HTTP 200 always — return what we have.
			writeJSON(w, http.StatusOK, resp)
			return
		}
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
		// total_items is the full per-user item count (Fever uses it to gauge
		// paging progress), not the size of this page.
		if total, terr := d.Store.FeverTotalItems(r.Context(), user.ID); terr == nil {
			resp["total_items"] = total
		} else {
			resp["total_items"] = len(out)
		}
	}

	if mark := r.FormValue("mark"); mark != "" {
		as := r.FormValue("as")
		idStr := r.FormValue("id")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		if mark == "item" && id > 0 {
			var markErr error
			switch as {
			case "read":
				markErr = d.Store.SetRead(r.Context(), user.ID, []int64{id}, true)
			case "unread":
				markErr = d.Store.SetRead(r.Context(), user.ID, []int64{id}, false)
			case "saved":
				markErr = d.Store.SetStarred(r.Context(), user.ID, id, true)
			case "unsaved":
				markErr = d.Store.SetStarred(r.Context(), user.ID, id, false)
			}
			// Fever clients always expect 200; log the error but don't change
			// the response so the client doesn't treat it as auth failure.
			if markErr != nil {
				slog.Default().Warn("fever: mark item failed",
					"user_id", user.ID, "mark", mark, "as", as, "id", id, "err", markErr)
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (d *Dependencies) feverFindUser(ctx context.Context, apiKey string) (models.User, error) {
	if apiKey == "" {
		return models.User{}, store.ErrNotFound
	}
	// Direct indexed lookup instead of a full table scan. The constant-time
	// compare guards against timing attacks on the token comparison itself.
	u, err := d.Store.GetUserByFeverToken(ctx, apiKey)
	if err != nil {
		return models.User{}, store.ErrNotFound
	}
	// Constant-time compare confirms the token even after the indexed lookup,
	// preventing timing side-channels on the database comparison itself.
	if subtle.ConstantTimeCompare([]byte(apiKey), []byte(u.FeverToken)) != 1 {
		return models.User{}, store.ErrNotFound
	}
	return u, nil
}

// joinIDs renders an id slice as the comma-separated string the Fever protocol
// expects for unread_item_ids / saved_item_ids.
func joinIDs(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}

// parseFeverIDs parses a Fever with_ids value (comma-separated ids), dropping
// blanks and non-numeric entries and capping at the 50-item per-call ceiling.
func parseFeverIDs(s string) []int64 {
	out := make([]int64, 0, 50)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			continue
		}
		out = append(out, id)
		if len(out) == 50 {
			break
		}
	}
	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
