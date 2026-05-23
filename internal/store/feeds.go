package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/brandonhon/ember/internal/models"
)

// UpsertFeed inserts a feed by URL or returns the existing row. The returned
// Feed has its ID populated.
func (s *Store) UpsertFeed(ctx context.Context, f models.Feed) (models.Feed, error) {
	if f.CreatedAt == 0 {
		f.CreatedAt = s.nowUnix()
	}
	if f.FetchInterval == 0 {
		f.FetchInterval = 1800
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return models.Feed{}, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRowContext(ctx, `SELECT id FROM feeds WHERE url = ?`, f.URL)
	var id int64
	if err := row.Scan(&id); err == nil {
		f.ID = id
		if err := tx.Commit(); err != nil {
			return models.Feed{}, err
		}
		return s.GetFeed(ctx, id)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return models.Feed{}, err
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO feeds (url, site_url, title, favicon_url, etag, last_modified,
			last_fetched, next_fetch, fetch_interval, error_count, last_error, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.URL, nullable(f.SiteURL), f.Title, nullable(f.FaviconURL),
		nullable(f.ETag), nullable(f.LastModified),
		nullableInt(f.LastFetched), nullableInt(f.NextFetch),
		f.FetchInterval, f.ErrorCount, nullable(f.LastError), f.CreatedAt)
	if err != nil {
		return models.Feed{}, err
	}
	newID, err := res.LastInsertId()
	if err != nil {
		return models.Feed{}, err
	}
	f.ID = newID
	if err := tx.Commit(); err != nil {
		return models.Feed{}, err
	}
	return f, nil
}

// GetFeed returns a feed by id.
func (s *Store) GetFeed(ctx context.Context, id int64) (models.Feed, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, url, IFNULL(site_url,''), title, IFNULL(favicon_url,''),
		       IFNULL(etag,''), IFNULL(last_modified,''),
		       IFNULL(last_fetched,0), IFNULL(next_fetch,0),
		       fetch_interval, error_count, IFNULL(last_error,''), created_at
		FROM feeds WHERE id = ?`, id)
	return scanFeed(row)
}

// FeedsDue returns all feeds whose next_fetch is at or before the cutoff (or
// is NULL — i.e. never fetched). Used by the poller.
func (s *Store) FeedsDue(ctx context.Context, cutoff int64, limit int) ([]models.Feed, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, url, IFNULL(site_url,''), title, IFNULL(favicon_url,''),
		       IFNULL(etag,''), IFNULL(last_modified,''),
		       IFNULL(last_fetched,0), IFNULL(next_fetch,0),
		       fetch_interval, error_count, IFNULL(last_error,''), created_at
		FROM feeds
		WHERE next_fetch IS NULL OR next_fetch <= ?
		ORDER BY IFNULL(next_fetch,0)
		LIMIT ?`, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Feed
	for rows.Next() {
		f, err := scanFeed(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// UpdateFeedFetchPatch records the result of a poll cycle.
type UpdateFeedFetchPatch struct {
	ETag         *string
	LastModified *string
	LastFetched  int64
	NextFetch    int64
	ErrorCount   int
	LastError    string
	Title        *string
	SiteURL      *string
	FaviconURL   *string
}

// UpdateFeedFetch updates fetch bookkeeping fields for a feed.
func (s *Store) UpdateFeedFetch(ctx context.Context, feedID int64, p UpdateFeedFetchPatch) error {
	sets := []string{
		"last_fetched = ?", "next_fetch = ?", "error_count = ?", "last_error = ?",
	}
	args := []any{p.LastFetched, p.NextFetch, p.ErrorCount, nullable(p.LastError)}
	if p.ETag != nil {
		sets = append(sets, "etag = ?")
		args = append(args, nullable(*p.ETag))
	}
	if p.LastModified != nil {
		sets = append(sets, "last_modified = ?")
		args = append(args, nullable(*p.LastModified))
	}
	if p.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *p.Title)
	}
	if p.SiteURL != nil {
		sets = append(sets, "site_url = ?")
		args = append(args, nullable(*p.SiteURL))
	}
	if p.FaviconURL != nil {
		sets = append(sets, "favicon_url = ?")
		args = append(args, nullable(*p.FaviconURL))
	}
	args = append(args, feedID)
	res, err := s.DB.ExecContext(ctx,
		"UPDATE feeds SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Subscribe adds a subscription for the user. If the user is already
// subscribed, returns the existing subscription.
func (s *Store) Subscribe(ctx context.Context, sub models.Subscription) (models.Subscription, error) {
	sub.CreatedAt = s.nowUnix()
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO subscriptions (user_id, feed_id, category_id, title_override, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		sub.UserID, sub.FeedID, nullableInt64Ptr(sub.CategoryID),
		nullable(sub.TitleOverride), sub.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return s.GetSubscription(ctx, sub.UserID, sub.FeedID)
		}
		return models.Subscription{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Subscription{}, err
	}
	sub.ID = id
	return sub, nil
}

// GetSubscription returns the user's subscription to a feed.
func (s *Store) GetSubscription(ctx context.Context, userID, feedID int64) (models.Subscription, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, user_id, feed_id, category_id, IFNULL(title_override,''), created_at
		FROM subscriptions WHERE user_id = ? AND feed_id = ?`, userID, feedID)
	return scanSubscription(row)
}

// GetSubscriptionByID returns the subscription identified by sub.ID, scoped to
// the user (returns ErrNotFound on cross-user access).
func (s *Store) GetSubscriptionByID(ctx context.Context, userID, subID int64) (models.Subscription, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, user_id, feed_id, category_id, IFNULL(title_override,''), created_at
		FROM subscriptions WHERE id = ? AND user_id = ?`, subID, userID)
	return scanSubscription(row)
}

// UpdateSubscriptionPatch is a sparse patch.
type UpdateSubscriptionPatch struct {
	CategoryID    *int64 // pointer-to-pointer trick: nil = leave alone, *p=0 → set NULL, *p>0 → set
	ClearCategory bool
	TitleOverride *string
}

// UpdateSubscription updates a subscription's category or title override.
func (s *Store) UpdateSubscription(ctx context.Context, userID, subID int64, p UpdateSubscriptionPatch) error {
	sets := []string{}
	args := []any{}
	switch {
	case p.ClearCategory:
		sets = append(sets, "category_id = NULL")
	case p.CategoryID != nil:
		sets = append(sets, "category_id = ?")
		args = append(args, *p.CategoryID)
	}
	if p.TitleOverride != nil {
		sets = append(sets, "title_override = ?")
		args = append(args, nullable(*p.TitleOverride))
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, subID, userID)
	res, err := s.DB.ExecContext(ctx,
		"UPDATE subscriptions SET "+strings.Join(sets, ", ")+" WHERE id = ? AND user_id = ?", args...)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Unsubscribe deletes a user's subscription to a feed. The shared feed row is
// retained if any other user is still subscribed.
func (s *Store) Unsubscribe(ctx context.Context, userID, subID int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var feedID int64
	err = tx.QueryRowContext(ctx,
		`SELECT feed_id FROM subscriptions WHERE id = ? AND user_id = ?`, subID, userID).Scan(&feedID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM subscriptions WHERE id = ? AND user_id = ?`, subID, userID); err != nil {
		return err
	}
	// Drop the feed if no one else subscribes.
	var refs int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM subscriptions WHERE feed_id = ?`, feedID).Scan(&refs); err != nil {
		return err
	}
	if refs == 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM feeds WHERE id = ?`, feedID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListFeedsForUser returns the user's subscriptions joined with feed metadata
// and unread counts.
func (s *Store) ListFeedsForUser(ctx context.Context, userID int64) ([]models.FeedWithCounts, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT f.id, f.url, IFNULL(f.site_url,''), f.title, IFNULL(f.favicon_url,''),
		       IFNULL(f.etag,''), IFNULL(f.last_modified,''),
		       IFNULL(f.last_fetched,0), IFNULL(f.next_fetch,0),
		       f.fetch_interval, f.error_count, IFNULL(f.last_error,''), f.created_at,
		       s.id AS sub_id, s.category_id, IFNULL(s.title_override,''),
		       (SELECT COUNT(*)
		          FROM articles a
		          LEFT JOIN article_state st ON st.article_id = a.id AND st.user_id = s.user_id
		         WHERE a.feed_id = f.id AND IFNULL(st.is_read,0) = 0) AS unread
		FROM feeds f
		JOIN subscriptions s ON s.feed_id = f.id
		WHERE s.user_id = ?
		ORDER BY LOWER(IFNULL(NULLIF(s.title_override,''), f.title))`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.FeedWithCounts
	for rows.Next() {
		var f models.FeedWithCounts
		var catID sql.NullInt64
		err := rows.Scan(
			&f.ID, &f.URL, &f.SiteURL, &f.Title, &f.FaviconURL,
			&f.ETag, &f.LastModified, &f.LastFetched, &f.NextFetch,
			&f.FetchInterval, &f.ErrorCount, &f.LastError, &f.CreatedAt,
			&f.SubscriptionID, &catID, &f.TitleOverride, &f.Unread,
		)
		if err != nil {
			return nil, err
		}
		if catID.Valid {
			v := catID.Int64
			f.CategoryID = &v
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func scanFeed(row scannable) (models.Feed, error) {
	var f models.Feed
	err := row.Scan(
		&f.ID, &f.URL, &f.SiteURL, &f.Title, &f.FaviconURL,
		&f.ETag, &f.LastModified, &f.LastFetched, &f.NextFetch,
		&f.FetchInterval, &f.ErrorCount, &f.LastError, &f.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Feed{}, ErrNotFound
	}
	return f, err
}

func scanSubscription(row scannable) (models.Subscription, error) {
	var s models.Subscription
	var catID sql.NullInt64
	err := row.Scan(&s.ID, &s.UserID, &s.FeedID, &catID, &s.TitleOverride, &s.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Subscription{}, ErrNotFound
	}
	if err != nil {
		return models.Subscription{}, err
	}
	if catID.Valid {
		v := catID.Int64
		s.CategoryID = &v
	}
	return s, nil
}

func nullableInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullableInt64Ptr(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}
