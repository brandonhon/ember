-- +goose Up
-- +goose StatementBegin
ALTER TABLE subscriptions ADD COLUMN muted INTEGER NOT NULL DEFAULT 0;
CREATE INDEX idx_subs_user_muted ON subscriptions(user_id, muted);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_subs_user_muted;
-- SQLite < 3.35 cannot DROP COLUMN; safer to no-op the down here.
-- +goose StatementEnd
