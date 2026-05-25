-- +goose Up
CREATE TABLE saved_searches (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT    NOT NULL,
  query      TEXT    NOT NULL,
  created_at INTEGER NOT NULL,
  UNIQUE(user_id, name)
);
CREATE INDEX idx_saved_searches_user ON saved_searches(user_id);

-- +goose Down
DROP INDEX IF EXISTS idx_saved_searches_user;
DROP TABLE IF EXISTS saved_searches;
