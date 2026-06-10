package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/brandonhon/ember/internal/models"
)

// SearchResult is a single FTS hit.
type SearchResult struct {
	models.ArticleView
	Rank float64 `json:"rank"`
}

// Search runs an FTS5 query scoped to the user's subscriptions. Results are
// ranked by bm25 (lower rank = better match in FTS5; we negate for sorting).
//
// publishedAfter (unix seconds) bounds results to articles published at/after
// it — the search-window setting, capped at the retention window. Pass 0 to
// search the full retained set. offset pages the bm25-ranked results (the SPA
// loads 25 at a time via "Load more"); pass 0 for the first page.
func (s *Store) Search(ctx context.Context, userID int64, query string, limit int, publishedAfter int64, offset int) ([]SearchResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}
	if query == "" {
		return nil, nil
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT a.id, a.feed_id, a.guid, IFNULL(a.url,''), a.title, IFNULL(a.author,''),
		       IFNULL(a.content_html,''), IFNULL(a.content_text,''),
		       IFNULL(a.summary,''), IFNULL(a.summary_model,''),
		       IFNULL(a.image_url,''), IFNULL(a.published_at,0),
		       a.fetched_at, a.content_hash, IFNULL(a.tags,''),
		       IFNULL(st.is_read,0), IFNULL(st.is_starred,0), IFNULL(st.is_later,0),
		       bm25(articles_fts) AS rank
		FROM articles_fts
		JOIN articles a ON a.id = articles_fts.rowid
		JOIN subscriptions s ON s.feed_id = a.feed_id AND s.user_id = ?
		LEFT JOIN article_state st ON st.article_id = a.id AND st.user_id = ?
		WHERE articles_fts MATCH ?
		  AND IFNULL(a.published_at,0) >= ?
		ORDER BY rank
		LIMIT ? OFFSET ?`, userID, userID, query, publishedAfter, limit, offset)
	if err != nil {
		// A malformed MATCH expression (unbalanced quote, bare operator, bad
		// column filter) is a client mistake, not a server fault — surface it
		// as ErrInvalidQuery so the api layer returns 400 instead of 500.
		if isFTSQueryError(err) {
			return nil, fmt.Errorf("%w: %v", ErrInvalidQuery, err)
		}
		return nil, err
	}
	defer rows.Close()
	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		var ir, is, il int
		if err := rows.Scan(&r.ID, &r.FeedID, &r.GUID, &r.URL, &r.Title, &r.Author,
			&r.ContentHTML, &r.ContentText, &r.Summary, &r.SummaryModel,
			&r.ImageURL, &r.PublishedAt, &r.FetchedAt, &r.ContentHash, &r.Tags,
			&ir, &is, &il, &r.Rank); err != nil {
			return nil, err
		}
		r.IsRead = ir == 1
		r.IsStarred = is == 1
		r.IsLater = il == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

// isFTSQueryError reports whether err is a SQLite FTS5 MATCH-syntax error
// caused by malformed user input (vs. a genuine infrastructure fault). The
// phrases below are the full set observed from modernc.org/sqlite for bad
// queries: unbalanced quote, bare operator, unknown column filter, and the
// "special query" prefix-search error.
func isFTSQueryError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "fts5:") ||
		strings.Contains(msg, "unterminated string") ||
		strings.Contains(msg, "unknown special query") ||
		strings.Contains(msg, "no such column")
}
