-- +goose Up
-- +goose StatementBegin
-- Per-board summarization trigger (issue #199). Pinning an article to a board
-- is one of the "I mean to read this" signals, alongside starring and saving
-- for later, so by default it requests a summary for that article when the
-- server is in on-demand mode.
--
-- DEFAULT 1 so every existing board triggers. Set 0 on a board used purely for
-- filing — a link dump or an archive — so pinning there doesn't spend inference.
ALTER TABLE boards ADD COLUMN summarize INTEGER NOT NULL DEFAULT 1;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- SQLite < 3.35 cannot DROP COLUMN; no-op the down.
-- +goose StatementEnd
