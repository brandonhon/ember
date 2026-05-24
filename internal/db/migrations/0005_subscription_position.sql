-- +goose Up
-- Position column for drag-to-reorder feeds in the sidebar. NULL during the
-- transition (treated as a large sentinel below) so first-load order matches
-- the previous alphabetical sort until the user reorders.
ALTER TABLE subscriptions ADD COLUMN position INTEGER NOT NULL DEFAULT 0;
CREATE INDEX idx_subs_user_position ON subscriptions(user_id, position);

-- +goose Down
DROP INDEX IF EXISTS idx_subs_user_position;
ALTER TABLE subscriptions DROP COLUMN position;
