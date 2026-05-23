package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/brandonhon/ember/internal/models"
)

// CreateUser inserts a user and returns it with its ID populated.
func (s *Store) CreateUser(ctx context.Context, u models.User) (models.User, error) {
	u.CreatedAt = s.nowUnix()
	if u.SettingsJSON == "" {
		u.SettingsJSON = "{}"
	}
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO users (username, email, password_hash, is_admin, settings_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		u.Username, nullable(u.Email), u.PasswordHash, boolToInt(u.IsAdmin),
		u.SettingsJSON, u.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return models.User{}, ErrConflict
		}
		return models.User{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.User{}, err
	}
	u.ID = id
	return u, nil
}

// GetUser returns a user by ID.
func (s *Store) GetUser(ctx context.Context, id int64) (models.User, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, username, IFNULL(email,''), password_hash, is_admin, settings_json, created_at
		FROM users WHERE id = ?`, id)
	return scanUser(row)
}

// GetUserByUsername returns a user by username.
func (s *Store) GetUserByUsername(ctx context.Context, username string) (models.User, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, username, IFNULL(email,''), password_hash, is_admin, settings_json, created_at
		FROM users WHERE username = ?`, username)
	return scanUser(row)
}

// ListUsers returns all users ordered by id.
func (s *Store) ListUsers(ctx context.Context) ([]models.User, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, username, IFNULL(email,''), password_hash, is_admin, settings_json, created_at
		FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CountUsers returns the number of users (used by first-run bootstrap).
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// UpdateUserPatch is a sparse update payload — nil fields are not touched.
type UpdateUserPatch struct {
	Email        *string
	PasswordHash *string
	IsAdmin      *bool
	SettingsJSON *string
}

// UpdateUser applies the patch to the named user.
func (s *Store) UpdateUser(ctx context.Context, id int64, patch UpdateUserPatch) error {
	sets := []string{}
	args := []any{}
	if patch.Email != nil {
		sets = append(sets, "email = ?")
		args = append(args, nullable(*patch.Email))
	}
	if patch.PasswordHash != nil {
		sets = append(sets, "password_hash = ?")
		args = append(args, *patch.PasswordHash)
	}
	if patch.IsAdmin != nil {
		sets = append(sets, "is_admin = ?")
		args = append(args, boolToInt(*patch.IsAdmin))
	}
	if patch.SettingsJSON != nil {
		sets = append(sets, "settings_json = ?")
		args = append(args, *patch.SettingsJSON)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	res, err := s.DB.ExecContext(ctx,
		"UPDATE users SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteUser removes a user.
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanUser(row scannable) (models.User, error) {
	var u models.User
	var isAdmin int
	if err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &isAdmin, &u.SettingsJSON, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.User{}, ErrNotFound
		}
		return models.User{}, err
	}
	u.IsAdmin = isAdmin == 1
	return u, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// isUniqueViolation returns true if err is a SQLite UNIQUE constraint failure.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}
