package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/brandonhon/ember/internal/models"
)

// GetDigest returns the user's digest config. A user with no row yet gets a
// zero-valued (disabled) Digest with their user_id populated.
func (s *Store) GetDigest(ctx context.Context, userID int64) (models.UserDigest, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT user_id, enabled, view_kind, view_value, hour_utc, minute_utc,
		       last_sent_at, email_override
		FROM user_digests WHERE user_id = ?`, userID)
	var d models.UserDigest
	var enabled int
	err := row.Scan(&d.UserID, &enabled, &d.ViewKind, &d.ViewValue, &d.HourUTC, &d.MinuteUTC, &d.LastSentAt, &d.EmailOverride)
	if errors.Is(err, sql.ErrNoRows) {
		return models.UserDigest{UserID: userID, ViewKind: "smart", ViewValue: "fresh", HourUTC: 8}, nil
	}
	if err != nil {
		return models.UserDigest{}, err
	}
	d.Enabled = enabled == 1
	return d, nil
}

// UpsertDigest inserts or updates the user's digest row.
func (s *Store) UpsertDigest(ctx context.Context, d models.UserDigest) error {
	enabled := 0
	if d.Enabled {
		enabled = 1
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO user_digests (user_id, enabled, view_kind, view_value, hour_utc, minute_utc, last_sent_at, email_override)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
		  enabled = excluded.enabled,
		  view_kind = excluded.view_kind,
		  view_value = excluded.view_value,
		  hour_utc = excluded.hour_utc,
		  minute_utc = excluded.minute_utc,
		  email_override = excluded.email_override`,
		d.UserID, enabled, d.ViewKind, d.ViewValue, d.HourUTC, d.MinuteUTC, d.LastSentAt, d.EmailOverride)
	return err
}

// MarkDigestSent updates last_sent_at to the given timestamp.
func (s *Store) MarkDigestSent(ctx context.Context, userID, when int64) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE user_digests SET last_sent_at = ? WHERE user_id = ?`, when, userID)
	return err
}

// ListEnabledDigests returns every digest row with enabled=1. The runner
// scans these every tick to decide who's due. Cheap at our scale.
func (s *Store) ListEnabledDigests(ctx context.Context) ([]models.UserDigest, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT user_id, enabled, view_kind, view_value, hour_utc, minute_utc, last_sent_at, email_override
		FROM user_digests WHERE enabled = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.UserDigest
	for rows.Next() {
		var d models.UserDigest
		var enabled int
		if err := rows.Scan(&d.UserID, &enabled, &d.ViewKind, &d.ViewValue, &d.HourUTC, &d.MinuteUTC, &d.LastSentAt, &d.EmailOverride); err != nil {
			return nil, err
		}
		d.Enabled = enabled == 1
		out = append(out, d)
	}
	return out, rows.Err()
}
