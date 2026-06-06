-- +goose Up
-- Title-fingerprint clustering: catches syndication where two outlets
-- publish the same story under different URLs (different hosts, different
-- canonicalized paths) but with effectively the same headline. Used as
-- an OR-branch in the dedup predicate alongside cluster_id from 0013.
--
-- The 48h window is enforced in the dedup query, not the column. A
-- composite index on (title_fingerprint, published_at) supports the
-- "same fingerprint, within window" join efficiently. Partial-index
-- excludes empty fingerprints so rows with too-short / generic titles
-- never falsely cluster with each other.
ALTER TABLE articles ADD COLUMN title_fingerprint TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_articles_fp_pub
  ON articles(title_fingerprint, published_at)
  WHERE title_fingerprint <> '';

-- +goose Down
DROP INDEX IF EXISTS idx_articles_fp_pub;
