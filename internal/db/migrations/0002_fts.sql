-- +goose Up
-- +goose StatementBegin
CREATE VIRTUAL TABLE articles_fts USING fts5(
  title, content_text, author,
  content='articles', content_rowid='id',
  tokenize = 'porter unicode61'
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER articles_ai AFTER INSERT ON articles BEGIN
  INSERT INTO articles_fts(rowid, title, content_text, author)
  VALUES (new.id, new.title, new.content_text, new.author);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER articles_ad AFTER DELETE ON articles BEGIN
  INSERT INTO articles_fts(articles_fts, rowid, title, content_text, author)
  VALUES ('delete', old.id, old.title, old.content_text, old.author);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER articles_au AFTER UPDATE ON articles BEGIN
  INSERT INTO articles_fts(articles_fts, rowid, title, content_text, author)
  VALUES ('delete', old.id, old.title, old.content_text, old.author);
  INSERT INTO articles_fts(rowid, title, content_text, author)
  VALUES (new.id, new.title, new.content_text, new.author);
END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS articles_au;
DROP TRIGGER IF EXISTS articles_ad;
DROP TRIGGER IF EXISTS articles_ai;
DROP TABLE IF EXISTS articles_fts;
-- +goose StatementEnd
