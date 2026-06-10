package store

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"
)

// RecordLogin shifts a user's login timestamps on a genuine new login:
// prev_login_at becomes the old last_login_at, and last_login_at becomes now.
// prev_login_at is the anchor for the unread window ("everything new since you
// were last here"). Called from auth.CreateSession, through which every login
// path funnels (password, passkey, magic link, registration).
func (s *Store) RecordLogin(ctx context.Context, userID int64) error {
	now := s.nowUnix()
	_, err := s.DB.ExecContext(ctx,
		`UPDATE users SET prev_login_at = last_login_at, last_login_at = ? WHERE id = ?`,
		now, userID)
	return err
}

// UnreadCutoff returns the unix-seconds lower bound for a user's unread window.
// Articles published at or after the cutoff are eligible to count as unread.
//
// The window is anchored on the user's previous login so someone who has been
// away sees everything new since their last visit — but it is clamped to
// [reading window, RetentionHours]: never narrower than the admin's reading
// window (so a user who checks in constantly still sees a full reading window
// of unread, and the feed/category lists that share this cutoff honor the
// setting), never wider than the retention window (we can't count what's been
// pruned). This single value drives the All-Unread, per-feed, and per-category
// badges AND the feed/category/unread list views, so a badge can never disagree
// with the column it summarizes.
func (s *Store) UnreadCutoff(ctx context.Context, userID int64) int64 {
	now := s.nowUnix()
	floor := int64(s.ResolveReadingWindowHours(ctx, DefaultReadingWindowHours)) * 3600
	ceil := int64(RetentionHours) * 3600

	var prev int64
	// Best-effort: a query failure falls back to the window floor. Log it so a
	// persistent DB problem (vs. a first-login zero) isn't silently masked.
	if err := s.DB.QueryRowContext(ctx,
		`SELECT IFNULL(prev_login_at,0) FROM users WHERE id = ?`, userID).Scan(&prev); err != nil && !errors.Is(err, sql.ErrNoRows) {
		slog.Default().Warn("unread cutoff: prev-login query failed, using window floor", "user_id", userID, "err", err)
	}

	window := floor
	if prev > 0 {
		if since := now - prev; since > window {
			window = since
		}
	}
	if window > ceil {
		window = ceil
	}
	return now - window
}

// PruneArticles deletes articles older than olderThan (by published_at, falling
// back to fetched_at) that are NOT starred, saved-for-later, pinned to a board,
// or shared. Unlike Cleanup it does NOT VACUUM/optimize — it's the cheap, fixed
// retention sweep run on a schedule; disk compaction is left to the optional
// admin Cleanup. Returns the number of rows removed.
func (s *Store) PruneArticles(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoff := s.nowUnix() - int64(olderThan.Seconds())
	res, err := s.DB.ExecContext(ctx, `
		DELETE FROM articles
		WHERE IFNULL(published_at, fetched_at) < ?
		  AND id NOT IN (SELECT article_id FROM article_state WHERE is_starred = 1)
		  AND id NOT IN (SELECT article_id FROM article_state WHERE is_later = 1)
		  AND id NOT IN (SELECT article_id FROM board_articles)
		  AND id NOT IN (SELECT article_id FROM shares)
	`, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
