package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/brandonhon/ember/internal/models"
)

// CreateBoard creates a board for the user.
func (s *Store) CreateBoard(ctx context.Context, b models.Board) (models.Board, error) {
	b.CreatedAt = s.nowUnix()
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO boards (user_id, name, created_at) VALUES (?, ?, ?)`,
		b.UserID, b.Name, b.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return models.Board{}, ErrConflict
		}
		return models.Board{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Board{}, err
	}
	b.ID = id
	return b, nil
}

// GetBoard returns the user's board.
func (s *Store) GetBoard(ctx context.Context, userID, id int64) (models.Board, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, user_id, name, created_at FROM boards WHERE id = ? AND user_id = ?`,
		id, userID)
	var b models.Board
	if err := row.Scan(&b.ID, &b.UserID, &b.Name, &b.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Board{}, ErrNotFound
		}
		return models.Board{}, err
	}
	return b, nil
}

// ListBoards returns all the user's boards.
func (s *Store) ListBoards(ctx context.Context, userID int64) ([]models.Board, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, user_id, name, created_at FROM boards WHERE user_id = ? ORDER BY LOWER(name)`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Board
	for rows.Next() {
		var b models.Board
		if err := rows.Scan(&b.ID, &b.UserID, &b.Name, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// DeleteBoard removes a board (and via cascade its board_articles entries).
func (s *Store) DeleteBoard(ctx context.Context, userID, id int64) error {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM boards WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// AddArticleToBoard adds an article to one of the user's boards. The article
// must belong to a feed the user is subscribed to (cross-user privacy).
func (s *Store) AddArticleToBoard(ctx context.Context, userID, boardID, articleID int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var ok int
	err = tx.QueryRowContext(ctx,
		`SELECT 1 FROM boards WHERE id = ? AND user_id = ?`, boardID, userID).Scan(&ok)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	err = tx.QueryRowContext(ctx, `
		SELECT 1 FROM articles a
		JOIN subscriptions s ON s.feed_id = a.feed_id AND s.user_id = ?
		WHERE a.id = ?`, userID, articleID).Scan(&ok)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO board_articles (board_id, article_id, added_at)
		VALUES (?, ?, ?)
		ON CONFLICT(board_id, article_id) DO NOTHING`,
		boardID, articleID, s.nowUnix()); err != nil {
		return err
	}
	return tx.Commit()
}

// RemoveArticleFromBoard removes an article from one of the user's boards.
func (s *Store) RemoveArticleFromBoard(ctx context.Context, userID, boardID, articleID int64) error {
	res, err := s.DB.ExecContext(ctx, `
		DELETE FROM board_articles
		WHERE board_id = ? AND article_id = ?
		  AND board_id IN (SELECT id FROM boards WHERE user_id = ?)`,
		boardID, articleID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
