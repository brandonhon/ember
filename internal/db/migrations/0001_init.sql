-- +goose Up
-- +goose StatementBegin
CREATE TABLE users (
  id            INTEGER PRIMARY KEY,
  username      TEXT NOT NULL UNIQUE,
  email         TEXT,
  password_hash TEXT NOT NULL,
  is_admin      INTEGER NOT NULL DEFAULT 0,
  settings_json TEXT NOT NULL DEFAULT '{}',
  created_at    INTEGER NOT NULL
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  user_agent TEXT
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

CREATE TABLE feeds (
  id             INTEGER PRIMARY KEY,
  url            TEXT NOT NULL,
  site_url       TEXT,
  title          TEXT NOT NULL,
  favicon_url    TEXT,
  etag           TEXT,
  last_modified  TEXT,
  last_fetched   INTEGER,
  next_fetch     INTEGER,
  fetch_interval INTEGER NOT NULL DEFAULT 1800,
  error_count    INTEGER NOT NULL DEFAULT 0,
  last_error     TEXT,
  created_at     INTEGER NOT NULL,
  UNIQUE(url)
);
CREATE INDEX idx_feeds_next_fetch ON feeds(next_fetch);

CREATE TABLE categories (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  color      TEXT,
  position   INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  UNIQUE(user_id, name)
);

CREATE TABLE subscriptions (
  id             INTEGER PRIMARY KEY,
  user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  feed_id        INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
  category_id    INTEGER REFERENCES categories(id) ON DELETE SET NULL,
  title_override TEXT,
  created_at     INTEGER NOT NULL,
  UNIQUE(user_id, feed_id)
);
CREATE INDEX idx_subs_user ON subscriptions(user_id);
CREATE INDEX idx_subs_feed ON subscriptions(feed_id);

CREATE TABLE articles (
  id            INTEGER PRIMARY KEY,
  feed_id       INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
  guid          TEXT NOT NULL,
  url           TEXT,
  title         TEXT NOT NULL,
  author        TEXT,
  content_html  TEXT,
  content_text  TEXT,
  summary       TEXT,
  summary_model TEXT,
  image_url     TEXT,
  published_at  INTEGER,
  fetched_at    INTEGER NOT NULL,
  content_hash  TEXT NOT NULL,
  UNIQUE(feed_id, guid)
);
CREATE INDEX idx_articles_feed_pub ON articles(feed_id, published_at DESC);
CREATE INDEX idx_articles_hash ON articles(content_hash);

CREATE TABLE article_state (
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  article_id INTEGER NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
  is_read    INTEGER NOT NULL DEFAULT 0,
  is_starred INTEGER NOT NULL DEFAULT 0,
  is_later   INTEGER NOT NULL DEFAULT 0,
  read_at    INTEGER,
  starred_at INTEGER,
  PRIMARY KEY (user_id, article_id)
);
CREATE INDEX idx_state_user_star ON article_state(user_id, is_starred);
CREATE INDEX idx_state_user_later ON article_state(user_id, is_later);
CREATE INDEX idx_state_user_read ON article_state(user_id, is_read);

CREATE TABLE boards (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  UNIQUE(user_id, name)
);

CREATE TABLE board_articles (
  board_id   INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
  article_id INTEGER NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
  added_at   INTEGER NOT NULL,
  PRIMARY KEY (board_id, article_id)
);

CREATE TABLE filters (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  match_json TEXT NOT NULL,
  action     TEXT NOT NULL,
  enabled    INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL
);
CREATE INDEX idx_filters_user ON filters(user_id, enabled);

CREATE TABLE shares (
  id         INTEGER PRIMARY KEY,
  article_id INTEGER NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
  from_user  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  to_user    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  note       TEXT,
  created_at INTEGER NOT NULL,
  seen       INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_shares_to ON shares(to_user, seen);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS shares;
DROP TABLE IF EXISTS filters;
DROP TABLE IF EXISTS board_articles;
DROP TABLE IF EXISTS boards;
DROP TABLE IF EXISTS article_state;
DROP TABLE IF EXISTS articles;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS categories;
DROP TABLE IF EXISTS feeds;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
-- +goose StatementEnd
