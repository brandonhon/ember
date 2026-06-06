-- +goose Up
-- Unique index on fever_token so feverFindUser can do a direct lookup
-- instead of a full table scan. The partial index (WHERE fever_token != '')
-- skips rows without a token, keeping it tight.
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_fever_token
    ON users(fever_token)
    WHERE fever_token != '';

-- +goose Down
DROP INDEX IF EXISTS idx_users_fever_token;
