package store

import (
	"context"
	"strings"
)

// AddArticleTag adds a single tag (lowercased, trimmed). Idempotent: hitting
// the primary key returns no error and no row added.
func (s *Store) AddArticleTag(ctx context.Context, userID, articleID int64, tag string) error {
	t := normalizeTag(tag)
	if t == "" {
		return nil
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT OR IGNORE INTO article_tags (user_id, article_id, tag, created_at)
		 VALUES (?, ?, ?, ?)`,
		userID, articleID, t, s.nowUnix())
	return err
}

// RemoveArticleTag deletes one tag from one article (user-scoped).
func (s *Store) RemoveArticleTag(ctx context.Context, userID, articleID int64, tag string) error {
	t := normalizeTag(tag)
	if t == "" {
		return nil
	}
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM article_tags WHERE user_id = ? AND article_id = ? AND tag = ?`,
		userID, articleID, t)
	return err
}

// ListArticleTags returns the user's tags on a single article.
func (s *Store) ListArticleTags(ctx context.Context, userID, articleID int64) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT tag FROM article_tags WHERE user_id = ? AND article_id = ? ORDER BY tag`,
		userID, articleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListUserTags returns every distinct tag the user has ever applied, with the
// count of articles carrying each. Used to populate a "Tags" sidebar list.
func (s *Store) ListUserTags(ctx context.Context, userID int64) ([]TagWithCount, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT tag, COUNT(*) FROM article_tags WHERE user_id = ? GROUP BY tag ORDER BY tag`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TagWithCount
	for rows.Next() {
		var t TagWithCount
		if err := rows.Scan(&t.Tag, &t.Count); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TagWithCount is a tag + how many articles carry it (for this user).
type TagWithCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// ListArticleIDsByTag returns article IDs the user has tagged with the given
// tag. Used to render the "filter by tag" view.
func (s *Store) ListArticleIDsByTag(ctx context.Context, userID int64, tag string) ([]int64, error) {
	t := normalizeTag(tag)
	if t == "" {
		return nil, nil
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT article_id FROM article_tags WHERE user_id = ? AND tag = ?`,
		userID, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// normalizeTag lowercases, trims, and collapses internal whitespace so
// "AI ", " ai", and "Ai" all become "ai".
func normalizeTag(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}
