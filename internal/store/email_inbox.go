package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/brandonhon/ember/internal/emailinbox"
	"github.com/brandonhon/ember/internal/models"
)

// gracePeriodSeconds is how long a rotated handle keeps accepting mail
// after the user generated a new one. 7 days matches Feedbin and gives
// stragglers (Substack quarterly batches) time to roll over.
const gracePeriodSeconds = 7 * 24 * 60 * 60

// EmailInbox is the resolved inbox row plus the user-facing address.
// Domain is filled by the API handler from cfg.EmailDomain — the store
// doesn't know about the configured domain.
type EmailInbox struct {
	ID            int64  `json:"id"`
	UserID        int64  `json:"-"`
	FeedID        int64  `json:"feed_id"`
	Handle        string `json:"handle"`
	SupersededAt  int64  `json:"-"`
	CreatedAt     int64  `json:"created_at"`
}

// EnsureInbox returns the user's active inbox, creating both the
// synthetic feed and the email_inboxes row on first call. Idempotent —
// the second call returns the existing row.
//
// The synthetic feed has kind='email' and a non-fetchable url
// ("email-inbox://<handle>") so the poller's RSS pass skips it.
func (s *Store) EnsureInbox(ctx context.Context, userID int64) (EmailInbox, error) {
	// Active handle?
	inbox, err := s.getActiveInbox(ctx, userID)
	if err == nil {
		return inbox, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return EmailInbox{}, err
	}

	// First call — provision feed + inbox in a tx so we don't get half
	// a feed without an inbox row pointing at it.
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return EmailInbox{}, fmt.Errorf("ensure inbox: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	handle, err := emailinbox.GenerateHandle()
	if err != nil {
		return EmailInbox{}, err
	}
	now := s.nowUnix()
	feedURL := "email-inbox://" + handle
	res, err := tx.ExecContext(ctx, `
		INSERT INTO feeds (url, title, kind, last_fetched, fetch_interval, error_count, created_at)
		VALUES (?, ?, 'email', 0, 0, 0, ?)`,
		feedURL, "Newsletters", now)
	if err != nil {
		return EmailInbox{}, fmt.Errorf("ensure inbox: insert feed: %w", err)
	}
	feedID, err := res.LastInsertId()
	if err != nil {
		return EmailInbox{}, fmt.Errorf("ensure inbox: feed id: %w", err)
	}
	// Subscribe the user to the feed so the article-list query (which
	// joins on subscriptions) actually surfaces incoming newsletters.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO subscriptions (user_id, feed_id, category_id, title_override, created_at)
		VALUES (?, ?, NULL, NULL, ?)`, userID, feedID, now); err != nil {
		return EmailInbox{}, fmt.Errorf("ensure inbox: insert subscription: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO email_inboxes (user_id, feed_id, handle, superseded_at, created_at)
		VALUES (?, ?, ?, 0, ?)`, userID, feedID, handle, now); err != nil {
		return EmailInbox{}, fmt.Errorf("ensure inbox: insert inbox: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return EmailInbox{}, fmt.Errorf("ensure inbox: commit: %w", err)
	}
	return EmailInbox{UserID: userID, FeedID: feedID, Handle: handle, CreatedAt: now}, nil
}

// RotateInbox generates a new handle for the user and supersedes the
// old one with a 7-day grace cutoff. The synthetic feed keeps its id —
// only the inbox row rotates — so existing articles stay attached.
func (s *Store) RotateInbox(ctx context.Context, userID int64) (EmailInbox, error) {
	cur, err := s.getActiveInbox(ctx, userID)
	if err != nil {
		return EmailInbox{}, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return EmailInbox{}, fmt.Errorf("rotate inbox: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	newHandle, err := emailinbox.GenerateHandle()
	if err != nil {
		return EmailInbox{}, err
	}
	now := s.nowUnix()
	cutoff := now + gracePeriodSeconds
	if _, err := tx.ExecContext(ctx,
		`UPDATE email_inboxes SET superseded_at = ? WHERE id = ?`, cutoff, cur.ID); err != nil {
		return EmailInbox{}, fmt.Errorf("rotate inbox: supersede: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO email_inboxes (user_id, feed_id, handle, superseded_at, created_at)
		VALUES (?, ?, ?, 0, ?)`, userID, cur.FeedID, newHandle, now); err != nil {
		return EmailInbox{}, fmt.Errorf("rotate inbox: insert: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return EmailInbox{}, fmt.Errorf("rotate inbox: commit: %w", err)
	}
	return EmailInbox{UserID: userID, FeedID: cur.FeedID, Handle: newHandle, CreatedAt: now}, nil
}

// ResolveInbox looks up an active OR within-grace handle and returns
// the owning user + feed. The third return is false when no row
// matches; that's the SMTP server's "no such mailbox" signal.
func (s *Store) ResolveInbox(ctx context.Context, handle string) (int64, int64, bool, error) {
	now := s.nowUnix()
	var userID, feedID int64
	err := s.DB.QueryRowContext(ctx, `
		SELECT user_id, feed_id FROM email_inboxes
		WHERE handle = ? AND (superseded_at = 0 OR superseded_at > ?)`,
		handle, now).Scan(&userID, &feedID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, fmt.Errorf("resolve inbox: %w", err)
	}
	return userID, feedID, true, nil
}

// IngestEmail parses a raw RFC 5322 message and upserts it as an
// article on the user's email feed. Satisfies emailinbox.Ingester.
func (s *Store) IngestEmail(ctx context.Context, userID, feedID int64, raw []byte) error {
	art, err := emailinbox.ParseMessage(raw)
	if err != nil {
		return fmt.Errorf("ingest email: parse: %w", err)
	}
	art.FeedID = feedID
	art.FetchedAt = s.nowUnix()
	if _, _, err := s.UpsertArticle(ctx, art); err != nil {
		return fmt.Errorf("ingest email: upsert: %w", err)
	}
	_ = userID // reserved for future per-user metadata
	return nil
}

// getActiveInbox returns the user's active (non-superseded) handle, or
// ErrNotFound when no inbox exists yet.
func (s *Store) getActiveInbox(ctx context.Context, userID int64) (EmailInbox, error) {
	var inbox EmailInbox
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, user_id, feed_id, handle, superseded_at, created_at
		FROM email_inboxes
		WHERE user_id = ? AND superseded_at = 0
		ORDER BY id DESC LIMIT 1`, userID).Scan(
		&inbox.ID, &inbox.UserID, &inbox.FeedID, &inbox.Handle, &inbox.SupersededAt, &inbox.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return EmailInbox{}, ErrNotFound
	}
	if err != nil {
		return EmailInbox{}, fmt.Errorf("get active inbox: %w", err)
	}
	return inbox, nil
}

// SkipEmailFeedsInPoller is consulted by the poller to skip kind='email'
// rows — they have no fetchable URL.
func (s *Store) FeedKind(ctx context.Context, feedID int64) (string, error) {
	var kind string
	err := s.DB.QueryRowContext(ctx, `SELECT IFNULL(kind,'rss') FROM feeds WHERE id = ?`, feedID).Scan(&kind)
	if err != nil {
		return "", err
	}
	return kind, nil
}

// for the unused-imports check in tests / Go
var _ = models.Article{}
