-- +goose Up
-- WebAuthn passkeys (FIDO2 credentials) bound to a user. A user may have
-- multiple — one per device they want to sign in from.
CREATE TABLE passkeys (
  id              INTEGER PRIMARY KEY,
  user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  credential_id   BLOB    NOT NULL UNIQUE,
  public_key      BLOB    NOT NULL,
  attestation_typ TEXT    NOT NULL DEFAULT '',
  aaguid          BLOB    NOT NULL DEFAULT x'',
  sign_count      INTEGER NOT NULL DEFAULT 0,
  transports      TEXT    NOT NULL DEFAULT '',  -- comma-joined
  backup_eligible INTEGER NOT NULL DEFAULT 0,
  backup_state    INTEGER NOT NULL DEFAULT 0,
  name            TEXT    NOT NULL DEFAULT '',
  created_at      INTEGER NOT NULL,
  last_used_at    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_passkeys_user ON passkeys(user_id);

-- Short-lived WebAuthn ceremony state. Stores the challenge + user binding
-- between begin and finish calls. Rows older than 5 minutes are stale; the
-- cleanup happens lazily in the API.
CREATE TABLE webauthn_sessions (
  id         TEXT    PRIMARY KEY,
  user_id    INTEGER,                    -- nullable: login flow can be username-less
  data       BLOB    NOT NULL,           -- JSON-encoded webauthn.SessionData
  purpose    TEXT    NOT NULL,           -- "register" | "login"
  created_at INTEGER NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS webauthn_sessions;
DROP INDEX IF EXISTS idx_passkeys_user;
DROP TABLE IF EXISTS passkeys;
