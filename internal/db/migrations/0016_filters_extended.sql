-- +goose Up
-- Rules engine extension: per-rule priority + action payload (e.g. tag
-- name or board id for the new `tag` / `add_to_board` actions). Apply
-- orders matched rules by priority asc — lower priority numbers win
-- when two rules contradict on the same field.
ALTER TABLE filters ADD COLUMN priority     INTEGER NOT NULL DEFAULT 100;
ALTER TABLE filters ADD COLUMN action_value TEXT    NOT NULL DEFAULT '';
CREATE INDEX idx_filters_user_prio ON filters(user_id, enabled, priority);

-- +goose Down
DROP INDEX IF EXISTS idx_filters_user_prio;
