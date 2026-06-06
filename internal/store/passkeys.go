package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/brandonhon/ember/internal/models"
)

// AnyPasskeyExists returns true if any user has registered at least one
// passkey. Drives the public login-page gate on the "Sign in with passkey"
// button: when no passkeys exist server-wide, the button is hidden so users
// don't try a doomed flow. Reveals exactly one bit ("at least one passkey
// user on this server") which is intentional — a per-username variant would
// be a textbook account-enumeration oracle.
func (s *Store) AnyPasskeyExists(ctx context.Context) (bool, error) {
	var n int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT 1 FROM passkeys LIMIT 1`).Scan(&n); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ListPasskeys returns all passkeys for a user, newest first.
func (s *Store) ListPasskeys(ctx context.Context, userID int64) ([]models.Passkey, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, user_id, credential_id, public_key, attestation_typ, aaguid,
		       sign_count, transports, backup_eligible, backup_state, name,
		       created_at, last_used_at
		FROM passkeys
		WHERE user_id = ?
		ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Passkey
	for rows.Next() {
		var p models.Passkey
		var be, bs int
		if err := rows.Scan(&p.ID, &p.UserID, &p.CredentialID, &p.PublicKey,
			&p.AttestationTyp, &p.AAGUID, &p.SignCount, &p.Transports,
			&be, &bs, &p.Name, &p.CreatedAt, &p.LastUsedAt); err != nil {
			return nil, err
		}
		p.BackupEligible = be != 0
		p.BackupState = bs != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetPasskeyByCredentialID looks up a passkey by its raw credential ID.
func (s *Store) GetPasskeyByCredentialID(ctx context.Context, credID []byte) (models.Passkey, error) {
	var p models.Passkey
	var be, bs int
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, user_id, credential_id, public_key, attestation_typ, aaguid,
		       sign_count, transports, backup_eligible, backup_state, name,
		       created_at, last_used_at
		FROM passkeys WHERE credential_id = ?`, credID).Scan(
		&p.ID, &p.UserID, &p.CredentialID, &p.PublicKey, &p.AttestationTyp,
		&p.AAGUID, &p.SignCount, &p.Transports, &be, &bs, &p.Name,
		&p.CreatedAt, &p.LastUsedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Passkey{}, ErrNotFound
	}
	if err != nil {
		return models.Passkey{}, err
	}
	p.BackupEligible = be != 0
	p.BackupState = bs != 0
	return p, nil
}

// InsertPasskey persists a newly-registered passkey.
func (s *Store) InsertPasskey(ctx context.Context, p models.Passkey) (models.Passkey, error) {
	if p.CreatedAt == 0 {
		p.CreatedAt = s.nowUnix()
	}
	be, bs := 0, 0
	if p.BackupEligible {
		be = 1
	}
	if p.BackupState {
		bs = 1
	}
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO passkeys
		  (user_id, credential_id, public_key, attestation_typ, aaguid,
		   sign_count, transports, backup_eligible, backup_state, name, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.UserID, p.CredentialID, p.PublicKey, p.AttestationTyp, p.AAGUID,
		p.SignCount, p.Transports, be, bs, p.Name, p.CreatedAt)
	if err != nil {
		return models.Passkey{}, err
	}
	id, _ := res.LastInsertId()
	p.ID = id
	return p, nil
}

// UpdatePasskeyOnUse bumps sign count + last_used_at after a successful login.
func (s *Store) UpdatePasskeyOnUse(ctx context.Context, id int64, signCount uint32) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE passkeys
		SET sign_count = ?, last_used_at = ?
		WHERE id = ?`, signCount, s.nowUnix(), id)
	return err
}

// RenamePasskey updates the user-facing name. Scoped by user to prevent
// renaming another account's credential by guessing the ID.
func (s *Store) RenamePasskey(ctx context.Context, userID, id int64, name string) error {
	res, err := s.DB.ExecContext(ctx, `
		UPDATE passkeys SET name = ? WHERE id = ? AND user_id = ?`, name, id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeletePasskey removes a passkey owned by the given user.
func (s *Store) DeletePasskey(ctx context.Context, userID, id int64) error {
	res, err := s.DB.ExecContext(ctx, `
		DELETE FROM passkeys WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// WebAuthnSession is the persisted form of an in-flight registration or
// assertion ceremony.
type WebAuthnSession struct {
	ID        string
	UserID    sql.NullInt64
	Data      []byte
	Purpose   string
	CreatedAt int64
}

// PutWebAuthnSession stores an in-flight ceremony.
func (s *Store) PutWebAuthnSession(ctx context.Context, sess WebAuthnSession) error {
	if sess.CreatedAt == 0 {
		sess.CreatedAt = s.nowUnix()
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO webauthn_sessions (id, user_id, data, purpose, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		sess.ID, sess.UserID, sess.Data, sess.Purpose, sess.CreatedAt)
	return err
}

// TakeWebAuthnSession atomically reads and deletes an in-flight ceremony.
// Rows older than 5 minutes are treated as not found (defense against replay
// of leaked IDs). DELETE...RETURNING closes the SELECT+DELETE TOCTOU window so
// no two concurrent requests can consume the same session row.
func (s *Store) TakeWebAuthnSession(ctx context.Context, id string) (WebAuthnSession, error) {
	cutoff := s.Now().Add(-5 * time.Minute).Unix()
	var sess WebAuthnSession
	err := s.DB.QueryRowContext(ctx, `
		DELETE FROM webauthn_sessions
		WHERE id = ? AND created_at >= ?
		RETURNING id, user_id, data, purpose, created_at`, id, cutoff).Scan(
		&sess.ID, &sess.UserID, &sess.Data, &sess.Purpose, &sess.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return WebAuthnSession{}, ErrNotFound
	}
	return sess, err
}

// CleanupWebAuthnSessions removes ceremony rows older than 5 minutes.
func (s *Store) CleanupWebAuthnSessions(ctx context.Context) error {
	cutoff := s.Now().Add(-5 * time.Minute).Unix()
	_, err := s.DB.ExecContext(ctx, `DELETE FROM webauthn_sessions WHERE created_at < ?`, cutoff)
	return err
}
