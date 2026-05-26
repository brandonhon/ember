-- +goose Up
-- Random per-user Fever API token. Replaces the predictable
-- md5(username:user_id) scheme which leaked to anyone who could enumerate
-- usernames. Backfill happens lazily on first /api/me call (the auth
-- middleware writes a fresh token when this column is empty for a user).
ALTER TABLE users ADD COLUMN fever_token TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE users DROP COLUMN fever_token;
