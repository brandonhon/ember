package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// maxThrottleKeyLen bounds how much of a submitted username is used as the
// login_failures key. Nothing longer than this can match a real account in any
// practical deployment, so truncating caps the damage an attacker can do by
// spraying megabyte-long usernames to inflate the table. Truncation (rather
// than skipping the record) means long strings still share a bucket and stay
// throttled instead of becoming a free retry lane.
const maxThrottleKeyLen = 128

// LoginFailure is the throttle state for one submitted username: how many
// consecutive failures it has accrued and when the last one happened.
// The zero value means "no failures on record".
type LoginFailure struct {
	Fails      int
	LastFailAt int64
}

// throttleKey normalizes a submitted username into its login_failures key.
// Case is preserved because usernames are compared case-sensitively by
// GetUserByUsername — lowercasing here would merge the throttle state of two
// genuinely distinct accounts.
func throttleKey(username string) string {
	if len(username) > maxThrottleKeyLen {
		return username[:maxThrottleKeyLen]
	}
	return username
}

// GetLoginFailures returns the throttle state for a submitted username. A
// username with no failures on record yields the zero LoginFailure and a nil
// error — "never failed" is not an error condition.
func (s *Store) GetLoginFailures(ctx context.Context, username string) (LoginFailure, error) {
	var lf LoginFailure
	err := s.DB.QueryRowContext(ctx,
		`SELECT fails, last_fail_at FROM login_failures WHERE username = ?`,
		throttleKey(username)).Scan(&lf.Fails, &lf.LastFailAt)
	if errors.Is(err, sql.ErrNoRows) {
		return LoginFailure{}, nil
	}
	if err != nil {
		return LoginFailure{}, err
	}
	return lf, nil
}

// RecordLoginFailure increments the consecutive-failure count for a submitted
// username and stamps the failure time, returning the new count. The upsert is
// a single statement so concurrent attempts can't lose an increment.
func (s *Store) RecordLoginFailure(ctx context.Context, username string) (int, error) {
	now := s.nowUnix()
	var fails int
	err := s.DB.QueryRowContext(ctx, `
		INSERT INTO login_failures (username, fails, last_fail_at)
		VALUES (?, 1, ?)
		ON CONFLICT(username) DO UPDATE SET
			fails = login_failures.fails + 1,
			last_fail_at = excluded.last_fail_at
		RETURNING fails`, throttleKey(username), now).Scan(&fails)
	if err != nil {
		return 0, err
	}
	return fails, nil
}

// ClearLoginFailures drops the throttle state for a username. Called after a
// successful authentication so a legitimate user who fat-fingered their
// password a few times starts clean.
func (s *Store) ClearLoginFailures(ctx context.Context, username string) error {
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM login_failures WHERE username = ?`, throttleKey(username))
	return err
}

// PruneLoginFailures deletes throttle rows whose last failure is older than
// age. Any such row has long since decayed to a zero backoff, so dropping it
// changes no decision — it just keeps the table from accumulating one row per
// username an attacker ever guessed.
func (s *Store) PruneLoginFailures(ctx context.Context, age time.Duration) (int64, error) {
	cutoff := s.Now().Add(-age).Unix()
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM login_failures WHERE last_fail_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
