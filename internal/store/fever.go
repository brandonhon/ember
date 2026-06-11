package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/brandonhon/ember/internal/models"
)

// Fever-shim store queries. The Fever protocol is a per-feed sync surface, NOT
// the SPA's reading surface, so these deliberately do NOT apply the cross-feed
// dedup, published-after window, or summary gate that buildArticleFilter
// (articles.go) applies for the web UI. Reasons:
//
//   - unread_item_ids / saved_item_ids are the AUTHORITATIVE, COMPLETE id sets a
//     Fever client diffs against its local cache. Capping them (the old code
//     capped at 200) silently truncates a backlog; cross-feed dedup drops real
//     unread items that live in a second feed. Either makes the client's unread
//     tally disagree with what Ember actually holds.
//   - items must reference exactly those ids, so it shares the same predicate
//     (non-deduped, per-feed) and is paged by id the way Fever clients expect.
//
// Muted subscriptions are excluded throughout, matching the SPA's treatment of
// muted feeds (the user has signalled they don't want them) and the Fever feed
// list semantics.

// FeverItemIDs returns ALL of the user's article ids matching flag across their
// non-muted subscriptions, ascending by id, with no dedup / window / cap. flag
// is "unread" (is_read = 0) or "saved" (is_starred = 1).
func (s *Store) FeverItemIDs(ctx context.Context, userID int64, flag string) ([]int64, error) {
	cond := "IFNULL(st.is_read,0) = 0"
	if flag == "saved" {
		cond = "IFNULL(st.is_starred,0) = 1"
	}
	rows, err := s.DB.QueryContext(ctx, fmt.Sprintf(`
SELECT a.id
FROM articles a
JOIN subscriptions sub ON sub.feed_id = a.feed_id AND sub.user_id = ? AND sub.muted = 0
LEFT JOIN article_state st ON st.article_id = a.id AND st.user_id = ?
WHERE %s
ORDER BY a.id ASC`, cond), userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// FeverItemQuery selects a page of Fever items. The mode is chosen in priority
// order: WithIDs (an explicit set the client asked for), then SinceID (id >
// SinceID ascending — the forward-sync path), then MaxID (id < MaxID descending
// — backfill), else the most recent page. Limit is clamped to [1, 50] (the
// Fever per-call ceiling).
type FeverItemQuery struct {
	WithIDs []int64
	SinceID int64
	MaxID   int64
	Limit   int
}

// FeverItems returns a page of the user's articles (read AND unread) across
// non-muted subscriptions, with NO cross-feed dedup so every id returned by
// FeverItemIDs is fetchable here. See the package-level note above.
func (s *Store) FeverItems(ctx context.Context, userID int64, q FeverItemQuery) ([]models.ArticleView, error) {
	if q.Limit <= 0 || q.Limit > 50 {
		q.Limit = 50
	}
	query := `
SELECT a.id, a.feed_id, IFNULL(a.url,''), a.title, IFNULL(a.author,''),
       IFNULL(a.content_html,''), IFNULL(a.published_at,0),
       IFNULL(st.is_read,0), IFNULL(st.is_starred,0)
FROM articles a
JOIN subscriptions sub ON sub.feed_id = a.feed_id AND sub.user_id = ? AND sub.muted = 0
LEFT JOIN article_state st ON st.article_id = a.id AND st.user_id = ?`
	args := []any{userID, userID}
	switch {
	case len(q.WithIDs) > 0:
		ph := make([]string, len(q.WithIDs))
		for i, id := range q.WithIDs {
			ph[i] = "?"
			args = append(args, id)
		}
		query += "\nWHERE a.id IN (" + strings.Join(ph, ",") + ")\nORDER BY a.id ASC"
	case q.SinceID > 0:
		query += "\nWHERE a.id > ?\nORDER BY a.id ASC"
		args = append(args, q.SinceID)
	case q.MaxID > 0:
		query += "\nWHERE a.id < ?\nORDER BY a.id DESC"
		args = append(args, q.MaxID)
	default:
		query += "\nORDER BY a.id DESC"
	}
	query += "\nLIMIT ?"
	args = append(args, q.Limit)

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ArticleView{}
	for rows.Next() {
		var v models.ArticleView
		var ir, is int
		if err := rows.Scan(&v.ID, &v.FeedID, &v.URL, &v.Title, &v.Author,
			&v.ContentHTML, &v.PublishedAt, &ir, &is); err != nil {
			return nil, err
		}
		v.IsRead = ir == 1
		v.IsStarred = is == 1
		out = append(out, v)
	}
	return out, rows.Err()
}

// FeverTotalItems returns the total number of items stored for the user across
// their non-muted subscriptions. Fever clients read total_items to know how
// many items exist so they can page through with since_id. (The old shim set
// total_items to the size of the returned page, which made paging progress
// meaningless.)
func (s *Store) FeverTotalItems(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM articles a
JOIN subscriptions sub ON sub.feed_id = a.feed_id AND sub.user_id = ? AND sub.muted = 0`,
		userID).Scan(&n)
	return n, err
}
