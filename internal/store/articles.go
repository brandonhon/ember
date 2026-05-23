package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/brandonhon/ember/internal/models"
)

// UpsertArticle inserts an article. If an article with the same (feed_id, guid)
// already exists OR the same content_hash exists within the feed, the existing
// row is returned and no new row is inserted (dedup). Returns (article, inserted).
func (s *Store) UpsertArticle(ctx context.Context, a models.Article) (models.Article, bool, error) {
	if a.FetchedAt == 0 {
		a.FetchedAt = s.nowUnix()
	}
	if a.ContentHash == "" {
		return models.Article{}, false, errors.New("UpsertArticle: ContentHash required")
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return models.Article{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	// Dedup #1: same (feed_id, guid).
	var existingID int64
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM articles WHERE feed_id = ? AND guid = ?`, a.FeedID, a.GUID).Scan(&existingID)
	if err == nil {
		if err := tx.Commit(); err != nil {
			return models.Article{}, false, err
		}
		out, gerr := s.GetArticle(ctx, existingID)
		return out, false, gerr
	} else if !errors.Is(err, sql.ErrNoRows) {
		return models.Article{}, false, err
	}

	// Dedup #2: same content_hash within feed (catches re-published items with
	// a fresh GUID).
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM articles WHERE feed_id = ? AND content_hash = ?`,
		a.FeedID, a.ContentHash).Scan(&existingID)
	if err == nil {
		if err := tx.Commit(); err != nil {
			return models.Article{}, false, err
		}
		out, gerr := s.GetArticle(ctx, existingID)
		return out, false, gerr
	} else if !errors.Is(err, sql.ErrNoRows) {
		return models.Article{}, false, err
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO articles (feed_id, guid, url, title, author, content_html,
			content_text, summary, summary_model, image_url, published_at,
			fetched_at, content_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.FeedID, a.GUID, nullable(a.URL), a.Title, nullable(a.Author),
		nullable(a.ContentHTML), nullable(a.ContentText), nullable(a.Summary),
		nullable(a.SummaryModel), nullable(a.ImageURL),
		nullableInt(a.PublishedAt), a.FetchedAt, a.ContentHash)
	if err != nil {
		if isUniqueViolation(err) {
			// Race: someone else inserted between our check and write. Fetch and
			// return as a non-insert.
			_ = tx.Commit()
			var id int64
			rerr := s.DB.QueryRowContext(ctx,
				`SELECT id FROM articles WHERE feed_id = ? AND guid = ?`, a.FeedID, a.GUID).Scan(&id)
			if rerr != nil {
				return models.Article{}, false, rerr
			}
			out, gerr := s.GetArticle(ctx, id)
			return out, false, gerr
		}
		return models.Article{}, false, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Article{}, false, err
	}
	a.ID = id
	if err := tx.Commit(); err != nil {
		return models.Article{}, false, err
	}
	return a, true, nil
}

// GetArticle returns an article by id (no per-user state).
func (s *Store) GetArticle(ctx context.Context, id int64) (models.Article, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, feed_id, guid, IFNULL(url,''), title, IFNULL(author,''),
		       IFNULL(content_html,''), IFNULL(content_text,''),
		       IFNULL(summary,''), IFNULL(summary_model,''),
		       IFNULL(image_url,''), IFNULL(published_at,0),
		       fetched_at, content_hash
		FROM articles WHERE id = ?`, id)
	return scanArticle(row)
}

// GetArticleForUser returns an article only if the user is subscribed to its
// feed (cross-user privacy).
func (s *Store) GetArticleForUser(ctx context.Context, userID, articleID int64) (models.ArticleView, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT a.id, a.feed_id, a.guid, IFNULL(a.url,''), a.title, IFNULL(a.author,''),
		       IFNULL(a.content_html,''), IFNULL(a.content_text,''),
		       IFNULL(a.summary,''), IFNULL(a.summary_model,''),
		       IFNULL(a.image_url,''), IFNULL(a.published_at,0),
		       a.fetched_at, a.content_hash,
		       IFNULL(st.is_read,0), IFNULL(st.is_starred,0), IFNULL(st.is_later,0)
		FROM articles a
		JOIN subscriptions s ON s.feed_id = a.feed_id AND s.user_id = ?
		LEFT JOIN article_state st ON st.article_id = a.id AND st.user_id = ?
		WHERE a.id = ?`, userID, userID, articleID)
	var v models.ArticleView
	var ir, is, il int
	err := row.Scan(&v.ID, &v.FeedID, &v.GUID, &v.URL, &v.Title, &v.Author,
		&v.ContentHTML, &v.ContentText, &v.Summary, &v.SummaryModel,
		&v.ImageURL, &v.PublishedAt, &v.FetchedAt, &v.ContentHash,
		&ir, &is, &il)
	if errors.Is(err, sql.ErrNoRows) {
		return models.ArticleView{}, ErrNotFound
	}
	if err != nil {
		return models.ArticleView{}, err
	}
	v.IsRead = ir == 1
	v.IsStarred = is == 1
	v.IsLater = il == 1
	return v, nil
}

// UpdateSummary sets the summary for an article.
func (s *Store) UpdateSummary(ctx context.Context, articleID int64, summary, model string) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE articles SET summary = ?, summary_model = ? WHERE id = ?`,
		summary, nullable(model), articleID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListArticlesQuery parameterizes a user article list.
type ListArticlesQuery struct {
	View       string // today|fresh|unread|starred|later|shared (optional)
	FeedID     int64
	CategoryID int64
	BoardID    int64
	Unread     bool
	Starred    bool
	Later      bool
	FreshAfter int64 // unix seconds; if set, requires published_at >= this
	Limit      int
	// Keyset cursor: (publishedBefore, idBefore). Pass zero values for the
	// first page.
	PublishedBefore int64
	IDBefore        int64
}

// ListArticles returns articles for the user under the given filters using
// keyset pagination on (published_at DESC, id DESC).
func (s *Store) ListArticles(ctx context.Context, userID int64, q ListArticlesQuery) ([]models.ArticleView, error) {
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}

	var (
		conds []string
		args  []any
	)

	// Source clause: shared-with-me uses a different join; other views all
	// scope to the user's subscriptions.
	from := `
FROM articles a
JOIN subscriptions s ON s.feed_id = a.feed_id AND s.user_id = ?
LEFT JOIN article_state st ON st.article_id = a.id AND st.user_id = ?`
	args = append(args, userID, userID)

	switch q.View {
	case "shared":
		from = `
FROM articles a
JOIN shares sh ON sh.article_id = a.id AND sh.to_user = ?
LEFT JOIN article_state st ON st.article_id = a.id AND st.user_id = ?`
		args = args[:0]
		args = append(args, userID, userID)
	case "starred":
		q.Starred = true
	case "later":
		q.Later = true
	case "unread":
		q.Unread = true
	case "fresh":
		// FreshAfter expected to be set by caller
	case "today":
		// caller can set FreshAfter to start-of-day
	}

	if q.FeedID > 0 {
		conds = append(conds, "a.feed_id = ?")
		args = append(args, q.FeedID)
	}
	if q.CategoryID > 0 {
		conds = append(conds, "s.category_id = ?")
		args = append(args, q.CategoryID)
	}
	if q.BoardID > 0 {
		from += `
JOIN board_articles ba ON ba.article_id = a.id
JOIN boards b ON b.id = ba.board_id AND b.user_id = ? AND b.id = ?`
		args = append(args, userID, q.BoardID)
	}
	if q.Unread {
		conds = append(conds, "IFNULL(st.is_read,0) = 0")
	}
	if q.Starred {
		conds = append(conds, "IFNULL(st.is_starred,0) = 1")
	}
	if q.Later {
		conds = append(conds, "IFNULL(st.is_later,0) = 1")
	}
	if q.FreshAfter > 0 {
		conds = append(conds, "IFNULL(a.published_at,0) >= ?")
		args = append(args, q.FreshAfter)
	}
	if q.PublishedBefore > 0 || q.IDBefore > 0 {
		conds = append(conds, "(IFNULL(a.published_at,0) < ? OR (IFNULL(a.published_at,0) = ? AND a.id < ?))")
		args = append(args, q.PublishedBefore, q.PublishedBefore, q.IDBefore)
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	query := fmt.Sprintf(`
SELECT a.id, a.feed_id, a.guid, IFNULL(a.url,''), a.title, IFNULL(a.author,''),
       IFNULL(a.content_html,''), IFNULL(a.content_text,''),
       IFNULL(a.summary,''), IFNULL(a.summary_model,''),
       IFNULL(a.image_url,''), IFNULL(a.published_at,0),
       a.fetched_at, a.content_hash,
       IFNULL(st.is_read,0), IFNULL(st.is_starred,0), IFNULL(st.is_later,0)
%s
%s
ORDER BY IFNULL(a.published_at,0) DESC, a.id DESC
LIMIT ?`, from, where)
	args = append(args, q.Limit)

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.ArticleView
	for rows.Next() {
		var v models.ArticleView
		var ir, is, il int
		if err := rows.Scan(&v.ID, &v.FeedID, &v.GUID, &v.URL, &v.Title, &v.Author,
			&v.ContentHTML, &v.ContentText, &v.Summary, &v.SummaryModel,
			&v.ImageURL, &v.PublishedAt, &v.FetchedAt, &v.ContentHash,
			&ir, &is, &il); err != nil {
			return nil, err
		}
		v.IsRead = ir == 1
		v.IsStarred = is == 1
		v.IsLater = il == 1
		out = append(out, v)
	}
	return out, rows.Err()
}

// CountUnread returns the user's unread count, optionally scoped to a feed or
// category. Pass 0 to skip a filter.
func (s *Store) CountUnread(ctx context.Context, userID, feedID, categoryID int64) (int, error) {
	q := `
SELECT COUNT(*)
FROM articles a
JOIN subscriptions s ON s.feed_id = a.feed_id AND s.user_id = ?
LEFT JOIN article_state st ON st.article_id = a.id AND st.user_id = ?
WHERE IFNULL(st.is_read,0) = 0`
	args := []any{userID, userID}
	if feedID > 0 {
		q += " AND a.feed_id = ?"
		args = append(args, feedID)
	}
	if categoryID > 0 {
		q += " AND s.category_id = ?"
		args = append(args, categoryID)
	}
	var n int
	err := s.DB.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}

func scanArticle(row scannable) (models.Article, error) {
	var a models.Article
	err := row.Scan(&a.ID, &a.FeedID, &a.GUID, &a.URL, &a.Title, &a.Author,
		&a.ContentHTML, &a.ContentText, &a.Summary, &a.SummaryModel,
		&a.ImageURL, &a.PublishedAt, &a.FetchedAt, &a.ContentHash)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Article{}, ErrNotFound
	}
	return a, err
}
