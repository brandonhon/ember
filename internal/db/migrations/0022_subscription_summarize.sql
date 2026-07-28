-- +goose Up
-- +goose StatementBegin
-- Per-subscription opt-out from AI summarization (issue #163). Summaries are
-- expensive on local/CPU-only inference, and for some feeds they add nothing.
--
-- DEFAULT 1 (opt-out, not opt-in) so every existing subscription keeps today's
-- behaviour. Contrast `muted`, which defaults to 0.
--
-- No index: subscriptions already has UNIQUE(user_id, feed_id) and idx_subs_feed
-- (0001_init.sql), which serve both lookups this column needs — "does any
-- subscriber of feed F want summaries" and "does this user want them". Adding
-- (feed_id, summarize) would only add write amplification on subscribe/
-- unsubscribe.
ALTER TABLE subscriptions ADD COLUMN summarize INTEGER NOT NULL DEFAULT 1;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- SQLite < 3.35 cannot DROP COLUMN; no-op the down, same as 0003_mute.sql.
-- +goose StatementEnd
