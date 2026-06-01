package store

import (
	"context"
	"fmt"
	"time"

	"github.com/brandonhon/ember/internal/filters"
	"github.com/brandonhon/ember/internal/models"
)

// PreviewFilter counts how many of the user's articles over the
// preceding `sinceDays` (0 = 7 default; capped at 30) would match the
// given parsed Match. Used by the UI's "this rule would have matched N
// items in the last 7 days" preview button.
//
// Implementation walks the candidate rows in Go rather than encoding
// the match as SQL — the filter engine handles every op/field including
// regex and relative-date matches that don't translate cleanly to SQL.
// Row count is bounded by the time window AND the user's subscriptions,
// so this stays cheap even with a deep history.
func (s *Store) PreviewFilter(ctx context.Context, userID int64, m filters.Match, sinceDays int) (int, error) {
	if sinceDays <= 0 {
		sinceDays = 7
	}
	if sinceDays > 30 {
		sinceDays = 30
	}
	now := time.Now()
	cutoff := now.Add(-time.Duration(sinceDays) * 24 * time.Hour).Unix()

	rows, err := s.DB.QueryContext(ctx, `
		SELECT a.id, a.feed_id, a.title, IFNULL(a.author,''), IFNULL(a.content_text,''),
		       IFNULL(a.url,''), IFNULL(a.image_url,''), IFNULL(a.published_at,0),
		       IFNULL(a.tags,'')
		FROM articles a
		JOIN subscriptions sub ON sub.feed_id = a.feed_id AND sub.user_id = ?
		WHERE sub.muted = 0 AND IFNULL(a.published_at,0) >= ?`,
		userID, cutoff)
	if err != nil {
		return 0, fmt.Errorf("filter preview: query: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var a models.Article
		if err := rows.Scan(&a.ID, &a.FeedID, &a.Title, &a.Author, &a.ContentText,
			&a.URL, &a.ImageURL, &a.PublishedAt, &a.Tags); err != nil {
			return 0, fmt.Errorf("filter preview: scan: %w", err)
		}
		if filters.Matches(m, a, now) {
			count++
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("filter preview: iterate: %w", err)
	}
	return count, nil
}
