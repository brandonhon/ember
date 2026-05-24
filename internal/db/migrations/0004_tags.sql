-- +goose Up
-- +goose StatementBegin
ALTER TABLE articles ADD COLUMN tags TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- SQLite < 3.35 cannot DROP COLUMN; no-op on the down here.
-- +goose StatementEnd
