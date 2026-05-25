-- +goose Up
-- User-defined tags applied to individual articles. Tags are scoped per-user
-- (so different users can tag the same article with different labels).
CREATE TABLE article_tags (
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  article_id INTEGER NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
  tag        TEXT    NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, article_id, tag)
);
CREATE INDEX idx_article_tags_user_tag ON article_tags(user_id, tag);
CREATE INDEX idx_article_tags_article  ON article_tags(article_id);

-- +goose Down
DROP INDEX IF EXISTS idx_article_tags_article;
DROP INDEX IF EXISTS idx_article_tags_user_tag;
DROP TABLE IF EXISTS article_tags;
