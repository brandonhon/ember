-- +goose Up
-- Server-wide settings KV. Used for things like the active Ollama model so
-- the admin UI can switch it without an env-var change + restart.
CREATE TABLE app_settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS app_settings;
