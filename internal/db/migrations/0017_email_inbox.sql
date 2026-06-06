-- +goose Up
-- Email-newsletter inbox: each user gets a unique <handle>@<EMBER_EMAIL_DOMAIN>
-- address. Mail addressed to it lands as articles in a synthetic per-user
-- "email" feed so newsletter content participates in the same list / read /
-- star / share / filter / summarize pipeline as RSS articles.
--
-- A new `kind` column on feeds distinguishes RSS from email inboxes. Existing
-- rows default to 'rss' so the migration is non-breaking.
ALTER TABLE feeds ADD COLUMN kind TEXT NOT NULL DEFAULT 'rss';

CREATE TABLE email_inboxes (
  id            INTEGER PRIMARY KEY,
  user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  feed_id       INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
  handle        TEXT    NOT NULL UNIQUE,
  -- superseded_at == 0 means the handle is active. Any positive value is
  -- the unix-second cutoff after which the row stops accepting mail
  -- (7-day grace after a rotate). The lookup query enforces the cutoff.
  superseded_at INTEGER NOT NULL DEFAULT 0,
  created_at    INTEGER NOT NULL
);
CREATE INDEX idx_email_inbox_user ON email_inboxes(user_id);

-- +goose Down
DROP INDEX IF EXISTS idx_email_inbox_user;
DROP TABLE IF EXISTS email_inboxes;
