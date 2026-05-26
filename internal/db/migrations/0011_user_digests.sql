-- +goose Up
-- Per-user daily digest email setting. Sends a summary email at the user's
-- chosen UTC time, containing articles in their chosen view that landed
-- since the last send.
CREATE TABLE user_digests (
  user_id      INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  enabled      INTEGER NOT NULL DEFAULT 0,
  view_kind    TEXT    NOT NULL DEFAULT 'smart',   -- smart | feed | category | board
  view_value   TEXT    NOT NULL DEFAULT 'fresh',   -- smart-name or numeric id
  hour_utc     INTEGER NOT NULL DEFAULT 8,         -- 0-23
  minute_utc   INTEGER NOT NULL DEFAULT 0,         -- 0-59
  last_sent_at INTEGER NOT NULL DEFAULT 0,
  email_override TEXT  NOT NULL DEFAULT ''         -- empty → use users.email
);

-- +goose Down
DROP TABLE IF EXISTS user_digests;
