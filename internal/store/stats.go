package store

import (
	"context"
	"time"
)

// UserStats is a snapshot of the user's reading activity.
type UserStats struct {
	ArticlesReadToday int       `json:"articles_read_today"`
	ArticlesReadWeek  int       `json:"articles_read_week"`
	ArticlesReadMonth int       `json:"articles_read_month"`
	StarredTotal      int       `json:"starred_total"`
	LaterTotal        int       `json:"later_total"`
	Subscriptions     int       `json:"subscriptions"`
	TopFeeds          []TopFeed `json:"top_feeds"`
}

// TopFeed is a feed + the user's read count in the last 30 days. Sorted
// descending by count.
type TopFeed struct {
	FeedID    int64  `json:"feed_id"`
	Title     string `json:"title"`
	ReadCount int    `json:"read_count"`
}

// UserStatsSnapshot collects all the numbers shown on the Stats settings
// page. Intentionally a single round-trip with several small queries — none
// of them are expensive at our scale and the page is admin-curiosity only.
func (s *Store) UserStatsSnapshot(ctx context.Context, userID int64) (UserStats, error) {
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	weekStart := now.AddDate(0, 0, -7).Unix()
	monthStart := now.AddDate(0, -1, 0).Unix()

	var out UserStats
	row := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM article_state WHERE user_id = ? AND is_read = 1 AND read_at >= ?`,
		userID, todayStart)
	if err := row.Scan(&out.ArticlesReadToday); err != nil {
		return out, err
	}
	row = s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM article_state WHERE user_id = ? AND is_read = 1 AND read_at >= ?`,
		userID, weekStart)
	if err := row.Scan(&out.ArticlesReadWeek); err != nil {
		return out, err
	}
	row = s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM article_state WHERE user_id = ? AND is_read = 1 AND read_at >= ?`,
		userID, monthStart)
	if err := row.Scan(&out.ArticlesReadMonth); err != nil {
		return out, err
	}
	row = s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM article_state WHERE user_id = ? AND is_starred = 1`, userID)
	if err := row.Scan(&out.StarredTotal); err != nil {
		return out, err
	}
	row = s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM article_state WHERE user_id = ? AND is_later = 1`, userID)
	if err := row.Scan(&out.LaterTotal); err != nil {
		return out, err
	}
	row = s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM subscriptions WHERE user_id = ?`, userID)
	if err := row.Scan(&out.Subscriptions); err != nil {
		return out, err
	}

	// Top 10 feeds by read articles in the last 30 days.
	rows, err := s.DB.QueryContext(ctx, `
		SELECT a.feed_id, IFNULL(NULLIF(s.title_override,''), f.title) AS title, COUNT(*) AS n
		FROM article_state st
		JOIN articles a ON a.id = st.article_id
		JOIN feeds f ON f.id = a.feed_id
		JOIN subscriptions s ON s.feed_id = a.feed_id AND s.user_id = st.user_id
		WHERE st.user_id = ? AND st.is_read = 1 AND st.read_at >= ?
		GROUP BY a.feed_id
		ORDER BY n DESC
		LIMIT 10`,
		userID, monthStart)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var t TopFeed
		if err := rows.Scan(&t.FeedID, &t.Title, &t.ReadCount); err != nil {
			return out, err
		}
		out.TopFeeds = append(out.TopFeeds, t)
	}
	return out, rows.Err()
}
