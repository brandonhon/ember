-- +goose Up
-- +goose StatementBegin
-- Track per-user login times so the unread window can extend back to a user's
-- previous visit. last_login_at is the current session's login; prev_login_at
-- is the one before it — the anchor for "everything new since you were last
-- here". Both default 0 (never logged in / first login).
ALTER TABLE users ADD COLUMN last_login_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN prev_login_at INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- SQLite < 3.35 cannot DROP COLUMN; no-op the down.
-- +goose StatementEnd
