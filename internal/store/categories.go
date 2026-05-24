package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/brandonhon/ember/internal/models"
)

// CreateCategory inserts a category for the given user.
func (s *Store) CreateCategory(ctx context.Context, c models.Category) (models.Category, error) {
	c.CreatedAt = s.nowUnix()
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO categories (user_id, name, color, position, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		c.UserID, c.Name, nullable(c.Color), c.Position, c.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return models.Category{}, ErrConflict
		}
		return models.Category{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Category{}, err
	}
	c.ID = id
	return c, nil
}

// GetCategory returns the user's category by id. Returns ErrNotFound if the
// category doesn't exist OR belongs to a different user (intentional — we
// don't leak existence).
func (s *Store) GetCategory(ctx context.Context, userID, id int64) (models.Category, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, user_id, name, IFNULL(color,''), position, created_at
		FROM categories WHERE id = ? AND user_id = ?`, id, userID)
	return scanCategory(row)
}

// ListCategories returns all categories for a user ordered by position then name.
func (s *Store) ListCategories(ctx context.Context, userID int64) ([]models.Category, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, user_id, name, IFNULL(color,''), position, created_at
		FROM categories WHERE user_id = ? ORDER BY position, name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Category
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateCategoryPatch is a sparse patch.
type UpdateCategoryPatch struct {
	Name     *string
	Color    *string
	Position *int
}

// UpdateCategory patches the user's category.
func (s *Store) UpdateCategory(ctx context.Context, userID, id int64, patch UpdateCategoryPatch) error {
	sets := []string{}
	args := []any{}
	if patch.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *patch.Name)
	}
	if patch.Color != nil {
		sets = append(sets, "color = ?")
		args = append(args, nullable(*patch.Color))
	}
	if patch.Position != nil {
		sets = append(sets, "position = ?")
		args = append(args, *patch.Position)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id, userID)
	res, err := s.DB.ExecContext(ctx,
		"UPDATE categories SET "+strings.Join(sets, ", ")+" WHERE id = ? AND user_id = ?", args...)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ReorderCategories assigns positions 0..N-1 to the given category ids in
// the order supplied. Categories that belong to other users are silently
// ignored so a client can't reorder another user's folders.
func (s *Store) ReorderCategories(ctx context.Context, userID int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx,
		`UPDATE categories SET position = ? WHERE id = ? AND user_id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i, id := range ids {
		if _, err := stmt.ExecContext(ctx, i, id, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteCategory removes the user's category. Subscriptions filed under it
// will have their category_id NULLed via ON DELETE SET NULL.
func (s *Store) DeleteCategory(ctx context.Context, userID, id int64) error {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM categories WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanCategory(row scannable) (models.Category, error) {
	var c models.Category
	if err := row.Scan(&c.ID, &c.UserID, &c.Name, &c.Color, &c.Position, &c.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Category{}, ErrNotFound
		}
		return models.Category{}, err
	}
	return c, nil
}
