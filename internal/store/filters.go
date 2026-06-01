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
	if f.Priority == 0 {
		f.Priority = 100
	}
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO filters (user_id, name, match_json, action, enabled, created_at, priority, action_value)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		f.UserID, f.Name, f.MatchJSON, f.Action, boolToInt(f.Enabled), f.CreatedAt, f.Priority, f.ActionValue)
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

// filterSelectCols holds the canonical select projection so the three
// readers stay in sync.
const filterSelectCols = `id, user_id, name, match_json, action, enabled, created_at, priority, action_value`

// GetFilter returns the user's filter by id.
func (s *Store) GetFilter(ctx context.Context, userID, id int64) (models.Filter, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+filterSelectCols+` FROM filters WHERE id = ? AND user_id = ?`, id, userID)
	return scanFilter(row)
}

// ListFilters returns all the user's filters ordered by priority asc,
// then id asc — same order the poller sees them.
func (s *Store) ListFilters(ctx context.Context, userID int64) ([]models.Filter, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+filterSelectCols+` FROM filters WHERE user_id = ? ORDER BY priority ASC, id ASC`, userID)
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

// ListActiveFilters returns the user's enabled filters in priority
// order (used by the poller hot path). Index idx_filters_user_prio
// supports this query.
func (s *Store) ListActiveFilters(ctx context.Context, userID int64) ([]models.Filter, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+filterSelectCols+` FROM filters
		 WHERE user_id = ? AND enabled = 1
		 ORDER BY priority ASC, id ASC`, userID)
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
	Name        *string
	MatchJSON   *string
	Action      *string
	Enabled     *bool
	Priority    *int
	ActionValue *string
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
	if p.Priority != nil {
		sets = append(sets, "priority = ?")
		args = append(args, *p.Priority)
	}
	if p.ActionValue != nil {
		sets = append(sets, "action_value = ?")
		args = append(args, *p.ActionValue)
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
	if err := row.Scan(&f.ID, &f.UserID, &f.Name, &f.MatchJSON, &f.Action, &enabled, &f.CreatedAt, &f.Priority, &f.ActionValue); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Filter{}, ErrNotFound
		}
		return models.Filter{}, err
	}
	f.Enabled = enabled == 1
	return f, nil
}
