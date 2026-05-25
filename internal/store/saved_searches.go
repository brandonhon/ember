package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/brandonhon/ember/internal/models"
)

// CreateSavedSearch inserts a saved search. UNIQUE(user_id, name) is enforced
// at the DB level — a duplicate name returns ErrConflict.
func (s *Store) CreateSavedSearch(ctx context.Context, ss models.SavedSearch) (models.SavedSearch, error) {
	ss.CreatedAt = s.nowUnix()
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO saved_searches (user_id, name, query, created_at) VALUES (?, ?, ?, ?)`,
		ss.UserID, ss.Name, ss.Query, ss.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return models.SavedSearch{}, ErrConflict
		}
		return models.SavedSearch{}, err
	}
	id, _ := res.LastInsertId()
	ss.ID = id
	return ss, nil
}

// ListSavedSearches returns all saved searches for the user, ordered by name.
func (s *Store) ListSavedSearches(ctx context.Context, userID int64) ([]models.SavedSearch, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, user_id, name, query, created_at FROM saved_searches WHERE user_id = ? ORDER BY name`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.SavedSearch
	for rows.Next() {
		var ss models.SavedSearch
		if err := rows.Scan(&ss.ID, &ss.UserID, &ss.Name, &ss.Query, &ss.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ss)
	}
	return out, rows.Err()
}

// GetSavedSearch returns a saved search by ID, scoped to the user.
func (s *Store) GetSavedSearch(ctx context.Context, userID, id int64) (models.SavedSearch, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, user_id, name, query, created_at FROM saved_searches WHERE id = ? AND user_id = ?`,
		id, userID)
	var ss models.SavedSearch
	err := row.Scan(&ss.ID, &ss.UserID, &ss.Name, &ss.Query, &ss.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return models.SavedSearch{}, ErrNotFound
	}
	return ss, err
}

// DeleteSavedSearch removes a saved search, scoped to the user.
func (s *Store) DeleteSavedSearch(ctx context.Context, userID, id int64) error {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM saved_searches WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
