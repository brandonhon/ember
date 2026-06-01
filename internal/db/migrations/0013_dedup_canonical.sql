-- +goose Up
-- Cross-feed dedup upgrade: track a canonical (tracking-param-stripped,
-- normalized) form of each article URL plus a short content-addressable
-- cluster_id derived from it. The existing list query used to dedup by
-- exact URL match via a correlated NOT EXISTS subquery; switching the
-- dedup predicate to cluster_id gives an indexed equality join and lets
-- future work cluster by criteria other than URL (e.g. title fingerprint).
--
-- Existing rows start with empty strings; a startup backfill in Go
-- populates both columns for the historical articles. The partial index
-- on cluster_id excludes those empty-string rows so the index stays small
-- until backfill completes and so empty values never match each other.
ALTER TABLE articles ADD COLUMN canonical_url TEXT NOT NULL DEFAULT '';
ALTER TABLE articles ADD COLUMN cluster_id    TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_articles_cluster ON articles(cluster_id) WHERE cluster_id <> '';

-- +goose Down
DROP INDEX IF EXISTS idx_articles_cluster;
-- SQLite < 3.35 cannot DROP COLUMN; matches the precedent in 0004/0010.
