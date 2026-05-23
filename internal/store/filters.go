package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/brandonhon/ember/internal/models"
)

// CreateFilter creates a filter for the user.
func (s *Store) CreateFilter(ctx context.Context, f models.Filter) (models.Filter, error) {
	f.CreatedAt = s.nowUnix()
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO filters (user_id, name, match_json, action, enabled, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		f.UserID, f.Name, f.MatchJSON, f.Action, boolToInt(f.Enabled), f.CreatedAt)
	if err != nil {
		return models.Filter{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Filter{}, err
	}
	f.ID = id
	return f, nil
}

// GetFilter returns the user's filter by id.
func (s *Store) GetFilter(ctx context.Context, userID, id int64) (models.Filter, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, user_id, name, match_json, action, enabled, created_at
		FROM filters WHERE id = ? AND user_id = ?`, id, userID)
	return scanFilter(row)
}

// ListFilters returns all the user's filters.
func (s *Store) ListFilters(ctx context.Context, userID int64) ([]models.Filter, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, user_id, name, match_json, action, enabled, created_at
		FROM filters WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Filter
	for rows.Next() {
		f, err := scanFilter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ListActiveFilters returns the user's enabled filters (used by the poller).
func (s *Store) ListActiveFilters(ctx context.Context, userID int64) ([]models.Filter, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, user_id, name, match_json, action, enabled, created_at
		FROM filters WHERE user_id = ? AND enabled = 1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Filter
	for rows.Next() {
		f, err := scanFilter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// UpdateFilterPatch is a sparse patch.
type UpdateFilterPatch struct {
	Name      *string
	MatchJSON *string
	Action    *string
	Enabled   *bool
}

// UpdateFilter patches the user's filter.
func (s *Store) UpdateFilter(ctx context.Context, userID, id int64, p UpdateFilterPatch) error {
	sets := []string{}
	args := []any{}
	if p.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *p.Name)
	}
	if p.MatchJSON != nil {
		sets = append(sets, "match_json = ?")
		args = append(args, *p.MatchJSON)
	}
	if p.Action != nil {
		sets = append(sets, "action = ?")
		args = append(args, *p.Action)
	}
	if p.Enabled != nil {
		sets = append(sets, "enabled = ?")
		args = append(args, boolToInt(*p.Enabled))
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id, userID)
	res, err := s.DB.ExecContext(ctx,
		"UPDATE filters SET "+strings.Join(sets, ", ")+" WHERE id = ? AND user_id = ?", args...)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteFilter removes the user's filter.
func (s *Store) DeleteFilter(ctx context.Context, userID, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM filters WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanFilter(row scannable) (models.Filter, error) {
	var f models.Filter
	var enabled int
	if err := row.Scan(&f.ID, &f.UserID, &f.Name, &f.MatchJSON, &f.Action, &enabled, &f.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Filter{}, ErrNotFound
		}
		return models.Filter{}, err
	}
	f.Enabled = enabled == 1
	return f, nil
}
