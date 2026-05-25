-- +goose Up
-- Cleaned article body produced by the LLM (promo/ad content stripped).
-- Falls back to content_html/content_text when empty.
ALTER TABLE articles ADD COLUMN cleaned_html TEXT NOT NULL DEFAULT '';

-- Speed up cross-feed dedup (URL equality lookup) and the global "fresh"
-- ordering used by smart views.
CREATE INDEX IF NOT EXISTS idx_articles_url ON articles(url);
CREATE INDEX IF NOT EXISTS idx_articles_published_at ON articles(published_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_articles_published_at;
DROP INDEX IF EXISTS idx_articles_url;
ALTER TABLE articles DROP COLUMN cleaned_html;
