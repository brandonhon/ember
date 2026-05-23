package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/brandonhon/ember/internal/models"
)

// CreateShare records a share from one user to another. The article must
// belong to a feed the sender is subscribed to.
func (s *Store) CreateShare(ctx context.Context, sh models.Share) (models.Share, error) {
	sh.CreatedAt = s.nowUnix()

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return models.Share{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var ok int
	err = tx.QueryRowContext(ctx, `
		SELECT 1 FROM articles a
		JOIN subscriptions s ON s.feed_id = a.feed_id AND s.user_id = ?
		WHERE a.id = ?`, sh.FromUser, sh.ArticleID).Scan(&ok)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Share{}, ErrNotFound
	}
	if err != nil {
		return models.Share{}, err
	}
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id = ?`, sh.ToUser).Scan(&ok)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Share{}, ErrNotFound
	}
	if err != nil {
		return models.Share{}, err
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO shares (article_id, from_user, to_user, note, created_at, seen)
		VALUES (?, ?, ?, ?, ?, 0)`,
		sh.ArticleID, sh.FromUser, sh.ToUser, nullable(sh.Note), sh.CreatedAt)
	if err != nil {
		return models.Share{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Share{}, err
	}
	sh.ID = id
	if err := tx.Commit(); err != nil {
		return models.Share{}, err
	}
	return sh, nil
}

// Inbox returns shares received by the user, newest first.
func (s *Store) Inbox(ctx context.Context, userID int64, unseenOnly bool, limit int) ([]models.Share, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `
SELECT id, article_id, from_user, to_user, IFNULL(note,''), created_at, seen
FROM shares WHERE to_user = ?`
	if unseenOnly {
		q += " AND seen = 0"
	}
	q += " ORDER BY created_at DESC, id DESC LIMIT ?"
	rows, err := s.DB.QueryContext(ctx, q, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Share
	for rows.Next() {
		var sh models.Share
		var seen int
		if err := rows.Scan(&sh.ID, &sh.ArticleID, &sh.FromUser, &sh.ToUser, &sh.Note, &sh.CreatedAt, &seen); err != nil {
			return nil, err
		}
		sh.Seen = seen == 1
		out = append(out, sh)
	}
	return out, rows.Err()
}

// MarkShareSeen marks a share as seen for the recipient.
func (s *Store) MarkShareSeen(ctx context.Context, userID, shareID int64) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE shares SET seen = 1 WHERE id = ? AND to_user = ?`, shareID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
