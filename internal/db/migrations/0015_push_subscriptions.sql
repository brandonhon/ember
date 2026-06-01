-- +goose Up
-- Web Push (VAPID) subscriptions per user. One row per browser/device the
-- user has opted in from. The VAPID keypair itself lives in app_settings
-- (vapid_public_key, vapid_private_key) — runtime-generated on first
-- start, kept until manually rotated.
--
-- endpoint is the browser-side push service URL (Mozilla / Google / Apple).
-- p256dh + auth are the ECDH public key and auth secret the browser hands
-- us at subscribe time; both required to encrypt outbound pushes.
CREATE TABLE push_subscriptions (
  id          INTEGER PRIMARY KEY,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  endpoint    TEXT    NOT NULL UNIQUE,
  p256dh      TEXT    NOT NULL,
  auth        TEXT    NOT NULL,
  user_agent  TEXT    NOT NULL DEFAULT '',
  created_at  INTEGER NOT NULL
);
CREATE INDEX idx_push_subs_user ON push_subscriptions(user_id);

-- +goose Down
DROP INDEX IF EXISTS idx_push_subs_user;
DROP TABLE IF EXISTS push_subscriptions;
