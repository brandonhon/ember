package store

import (
	"context"
	"database/sql"
	"errors"
)

// GetAppSetting reads a value from the app_settings KV. Returns ("", nil) when
// the key is unset.
func (s *Store) GetAppSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.DB.QueryRowContext(ctx,
		`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// PutAppSetting upserts a value into the app_settings KV.
func (s *Store) PutAppSetting(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO app_settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		key, value, s.nowUnix())
	return err
}
