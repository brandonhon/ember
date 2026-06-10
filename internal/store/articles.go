package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/brandonhon/ember/internal/feed"
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
	// Defensive fill for callers that bypass feed.Parse (tests, manual
	// insertions, future ingest paths). The dedup predicate runs against
	// cluster_id, so an empty value means the article won't dedup correctly.
	if a.CanonicalURL == "" && a.URL != "" {
		a.CanonicalURL = feed.CanonicalURL(a.URL)
	}
	if a.ClusterID == "" {
		a.ClusterID = feed.ClusterID(a.CanonicalURL)
	}
	if a.TitleFingerprint == "" {
		a.TitleFingerprint = feed.TitleFingerprint(a.Title)
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
			fetched_at, content_hash, tags, canonical_url, cluster_id, title_fingerprint)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.FeedID, a.GUID, nullable(a.URL), a.Title, nullable(a.Author),
		nullable(a.ContentHTML), nullable(a.ContentText), nullable(a.Summary),
		nullable(a.SummaryModel), nullable(a.ImageURL),
		nullableInt(a.PublishedAt), a.FetchedAt, a.ContentHash, a.Tags,
		a.CanonicalURL, a.ClusterID, a.TitleFingerprint)
	if err != nil {
		if isUniqueViolation(err) {
			// Race: someone else inserted between our check and write. The INSERT
			// failed so this tx has no work to commit — release it explicitly and
			// propagate a rollback error (a swallowed commit/rollback failure here
			// would otherwise mask DB-level problems like context cancellation).
			if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				return models.Article{}, false, fmt.Errorf("UpsertArticle: rollback race path: %w", rbErr)
			}
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
		       IFNULL(content_html,''), IFNULL(content_text,''), IFNULL(cleaned_html,''),
		       IFNULL(summary,''), IFNULL(summary_model,''),
		       IFNULL(image_url,''), IFNULL(published_at,0),
		       fetched_at, content_hash, IFNULL(tags,'')
		FROM articles WHERE id = ?`, id)
	return scanArticle(row)
}

// GetArticleForUser returns an article only if the user is subscribed to its
// feed (cross-user privacy).
func (s *Store) GetArticleForUser(ctx context.Context, userID, articleID int64) (models.ArticleView, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT a.id, a.feed_id, a.guid, IFNULL(a.url,''), a.title, IFNULL(a.author,''),
		       IFNULL(a.content_html,''), IFNULL(a.content_text,''), IFNULL(a.cleaned_html,''),
		       IFNULL(a.summary,''), IFNULL(a.summary_model,''),
		       IFNULL(a.image_url,''), IFNULL(a.published_at,0),
		       a.fetched_at, a.content_hash, IFNULL(a.tags,''),
		       IFNULL(st.is_read,0), IFNULL(st.is_starred,0), IFNULL(st.is_later,0)
		FROM articles a
		JOIN subscriptions s ON s.feed_id = a.feed_id AND s.user_id = ?
		LEFT JOIN article_state st ON st.article_id = a.id AND st.user_id = ?
		WHERE a.id = ?`, userID, userID, articleID)
	var v models.ArticleView
	var ir, is, il int
	err := row.Scan(&v.ID, &v.FeedID, &v.GUID, &v.URL, &v.Title, &v.Author,
		&v.ContentHTML, &v.ContentText, &v.CleanedHTML, &v.Summary, &v.SummaryModel,
		&v.ImageURL, &v.PublishedAt, &v.FetchedAt, &v.ContentHash, &v.Tags,
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

// ClearAllSummaries clears summary_model on every article (admin-only). Used
// after a summarizer prompt change to force re-processing of existing rows.
// Returns the affected article IDs so the caller can enqueue them.
func (s *Store) ClearAllSummaries(ctx context.Context) ([]int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM articles WHERE IFNULL(summary_model,'') <> ''`)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE articles SET summary_model = NULL, summary = '' WHERE IFNULL(summary_model,'') <> ''`); err != nil {
		return nil, err
	}
	return ids, tx.Commit()
}

// ListUnsummarizedIDs returns articles that have not yet been processed by
// the summarizer (summary_model is NULL or empty). Used at poller startup to
// backfill the in-memory summary queue after a restart.
func (s *Store) ListUnsummarizedIDs(ctx context.Context, limit int) ([]int64, error) {
	if limit <= 0 {
		limit = 256
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id FROM articles WHERE IFNULL(summary_model,'') = '' ORDER BY id DESC LIMIT ?`,
		limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ResetSummariesByFeed clears summary_model on every article in the feed
// where it currently equals 'skipped'. Returns the affected article IDs so
// the poller can re-enqueue them for a fresh summarize attempt.
func (s *Store) ResetSummariesByFeed(ctx context.Context, feedID int64) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id FROM articles WHERE feed_id = ? AND summary_model = 'skipped'`, feedID)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE articles SET summary_model = NULL, summary = '' WHERE feed_id = ? AND summary_model = 'skipped'`,
		feedID); err != nil {
		return nil, err
	}
	return ids, nil
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

// UpdateCleanedHTML stores the LLM-produced ad-stripped article body.
func (s *Store) UpdateCleanedHTML(ctx context.Context, articleID int64, html string) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE articles SET cleaned_html = ? WHERE id = ?`,
		html, articleID)
	return err
}

// UpdateArticleContent replaces the body fields after a re-extract pass. Used
// by the on-demand readability re-run that backs the reader pane's
// "Re-extract" button. cleaned_html is intentionally cleared — it was the
// ad-stripped projection of the OLD body, and stale cleaned_html shown over
// fresh content_text confuses both the UI and the summarizer.
func (s *Store) UpdateArticleContent(ctx context.Context, articleID int64, contentText, contentHTML, imageURL string) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE articles SET content_text = ?, content_html = ?, image_url = ?, cleaned_html = '' WHERE id = ?`,
		contentText, contentHTML, imageURL, articleID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Article-list paging bounds. Lists page 50 at a time via the keyset cursor
// ("Load more"); MaxArticleListLimit is the hard ceiling on a client-supplied
// ?limit=, protecting the correlated dup_count subquery from an unbounded
// fan-out.
const (
	defaultArticleListLimit = 50
	// MaxArticleListLimit caps a caller-requested page size. Exported so the
	// API handler clamps ?limit= to the same ceiling the store enforces.
	MaxArticleListLimit = 1000
)

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
	// OnlySummarized restricts results to articles the summarizer has already
	// processed (success or 'skipped' marker). The SPA passes true so users
	// never see a story before the LLM has had a chance to look at it.
	// Defaults to false — tests, admin tools, and the Fever shim get
	// everything by default.
	OnlySummarized bool
	// Tag filters to articles the user has tagged with this label (joined
	// against article_tags). Empty = no filter.
	Tag string
}

// buildArticleFilter assembles the FROM + WHERE clauses (and their args, in
// positional order) shared by ListArticles and CountArticles so a count can
// never diverge from the list it summarizes. It applies the view's read/star/
// later flags, feed/category/board/tag scope, the published-after window, the
// summary gate, the muted-feed exclusion, and cross-feed dedup. It does NOT
// apply keyset-cursor paging (list-only) — pass q.PublishedBefore/IDBefore for
// that via ListArticles.
func (s *Store) buildArticleFilter(userID int64, q ListArticlesQuery, withCursor bool) (from, where string, args []any) {
	var conds []string

	// Source clause: shared-with-me uses a different join; other views all
	// scope to the user's subscriptions.
	from = `
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
		// Fresh = recent + unread, matching CountSmartViews.Fresh. Without
		// this, the list shows every article in the time window (read +
		// unread) while the sidebar badge counts only unread — users see
		// "Fresh 4" but click in and find 50 items.
		q.Unread = true
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
	if q.Tag != "" {
		from += `
JOIN article_tags atg ON atg.article_id = a.id AND atg.user_id = ? AND atg.tag = ?`
		args = append(args, userID, q.Tag)
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
	if withCursor && (q.PublishedBefore > 0 || q.IDBefore > 0) {
		conds = append(conds, "(IFNULL(a.published_at,0) < ? OR (IFNULL(a.published_at,0) = ? AND a.id < ?))")
		args = append(args, q.PublishedBefore, q.PublishedBefore, q.IDBefore)
	}
	if q.OnlySummarized {
		conds = append(conds, "a.summary_model IS NOT NULL AND a.summary_model <> ''")
	}
	// Muted feeds: excluded from smart views (fresh/today/unread/starred/later)
	// and category views; still visible when the user explicitly clicks the
	// feed (FeedID > 0). The 'shared' view is also unaffected since it uses
	// a different join.
	if q.View != "shared" && q.FeedID == 0 {
		conds = append(conds, "s.muted = 0")
	}

	// Cross-feed dedup: when two feeds the user subscribes to publish the
	// same article, only the lowest-id row wins. Two predicates OR'd:
	//   (a) same canonical cluster_id — exact URL after tracking-param
	//       stripping. Tightest match.
	//   (b) same title_fingerprint within a 48h window — catches wire
	//       stories republished by multiple outlets under different URLs.
	//       Window keeps headlines that happen to repeat across years
	//       ("Apple Q3 earnings") from collapsing across time.
	// Skipped for per-feed (user opened the feed and wants its contents
	// verbatim), shared (explicit one-off share), and board views (explicit
	// curation). Empty cluster_id AND empty title_fingerprint rows always
	// pass (no signal in either dimension).
	if q.View != "shared" && q.FeedID == 0 && q.BoardID == 0 {
		conds = append(conds, `(
			(IFNULL(a.cluster_id,'') = '' AND IFNULL(a.title_fingerprint,'') = '')
			OR NOT EXISTS (
				SELECT 1 FROM articles a3
				JOIN subscriptions s3 ON s3.feed_id = a3.feed_id AND s3.user_id = ?
				WHERE s3.muted = 0 AND a3.id < a.id AND (
					(a3.cluster_id = a.cluster_id AND a3.cluster_id <> '')
					OR (
						a3.title_fingerprint = a.title_fingerprint
						AND a3.title_fingerprint <> ''
						AND ABS(IFNULL(a3.published_at,0) - IFNULL(a.published_at,0)) < 172800
					)
				)
			)
		)`)
		args = append(args, userID)
	}

	where = ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	return from, where, args
}

// ListArticles returns articles for the user under the given filters using
// keyset pagination on (published_at DESC, id DESC).
func (s *Store) ListArticles(ctx context.Context, userID int64, q ListArticlesQuery) ([]models.ArticleView, error) {
	if q.Limit <= 0 || q.Limit > MaxArticleListLimit {
		q.Limit = defaultArticleListLimit
	}

	from, where, args := s.buildArticleFilter(userID, q, true)

	// dup_count: when an article has a cluster_id, count how many OTHER
	// articles in the same cluster the user is subscribed to via different
	// feeds. The dedup filter above keeps the lowest-id row, so this count
	// tells the SPA "this article also appeared in N other feeds you follow"
	// and lets it render a pill.
	query := fmt.Sprintf(`
SELECT a.id, a.feed_id, a.guid, IFNULL(a.url,''), a.title, IFNULL(a.author,''),
       IFNULL(a.content_html,''), IFNULL(a.content_text,''), IFNULL(a.cleaned_html,''),
       IFNULL(a.summary,''), IFNULL(a.summary_model,''),
       IFNULL(a.image_url,''), IFNULL(a.published_at,0),
       a.fetched_at, a.content_hash, IFNULL(a.tags,''),
       IFNULL(st.is_read,0), IFNULL(st.is_starred,0), IFNULL(st.is_later,0),
       CASE WHEN IFNULL(a.cluster_id,'') = '' AND IFNULL(a.title_fingerprint,'') = '' THEN 0 ELSE (
           SELECT COUNT(*) FROM articles a4
           JOIN subscriptions s4 ON s4.feed_id = a4.feed_id AND s4.user_id = ?
           WHERE s4.muted = 0 AND a4.id <> a.id AND (
               (a4.cluster_id = a.cluster_id AND a4.cluster_id <> '')
               OR (
                   a4.title_fingerprint = a.title_fingerprint
                   AND a4.title_fingerprint <> ''
                   AND ABS(IFNULL(a4.published_at,0) - IFNULL(a.published_at,0)) < 172800
               )
           )
       ) END AS dup_count
%s
%s
ORDER BY IFNULL(a.published_at,0) DESC, a.id DESC
LIMIT ?`, from, where)
	// dup_count parameter goes BEFORE the from/where args, so we have to
	// rebuild args carefully. Easier: prepend user id to args.
	args = append([]any{userID}, args...)
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
			&v.ContentHTML, &v.ContentText, &v.CleanedHTML, &v.Summary, &v.SummaryModel,
			&v.ImageURL, &v.PublishedAt, &v.FetchedAt, &v.ContentHash, &v.Tags,
			&ir, &is, &il, &v.DupCount); err != nil {
			return nil, err
		}
		v.IsRead = ir == 1
		v.IsStarred = is == 1
		v.IsLater = il == 1
		out = append(out, v)
	}
	return out, rows.Err()
}

// CountArticles returns how many articles match the same filter ListArticles
// would return for q (ignoring keyset paging + limit). Sharing
// buildArticleFilter guarantees a badge can never disagree with the list it
// summarizes — the long-standing "sidebar says 9, column shows 6" bug came
// from count queries that omitted the summary gate and cross-feed dedup the
// list applied.
func (s *Store) CountArticles(ctx context.Context, userID int64, q ListArticlesQuery) (int, error) {
	from, where, args := s.buildArticleFilter(userID, q, false)
	var n int
	err := s.DB.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(*)\n%s\n%s", from, where), args...).Scan(&n)
	return n, err
}

// CountUnread returns the user's unread count, optionally scoped to a feed or
// category. Pass 0 to skip a filter. Counts every article, including those
// not yet processed by the summarizer — use CountUnreadVisible for the
// user-facing badge that matches the list view.
func (s *Store) CountUnread(ctx context.Context, userID, feedID, categoryID int64) (int, error) {
	return s.countUnread(ctx, userID, feedID, categoryID, false)
}

// CountUnreadVisible is the same as CountUnread but only counts articles the
// summarizer has finished (success or 'skipped' marker). This is what drives
// the sidebar badges in the SPA.
func (s *Store) CountUnreadVisible(ctx context.Context, userID, feedID, categoryID int64) (int, error) {
	return s.countUnread(ctx, userID, feedID, categoryID, true)
}

func (s *Store) countUnread(ctx context.Context, userID, feedID, categoryID int64, onlySummarized bool) (int, error) {
	q := `
SELECT COUNT(*)
FROM articles a
JOIN subscriptions s ON s.feed_id = a.feed_id AND s.user_id = ?
LEFT JOIN article_state st ON st.article_id = a.id AND st.user_id = ?
WHERE IFNULL(st.is_read,0) = 0`
	args := []any{userID, userID}
	if onlySummarized {
		q += " AND a.summary_model IS NOT NULL AND a.summary_model <> ''"
	}
	// When scoped to a single feed the caller is asking for that feed
	// specifically (e.g., the sidebar feed-row badge); honor it even if muted.
	// For aggregate counts (across feeds), muted subscriptions don't count.
	if feedID > 0 {
		q += " AND a.feed_id = ?"
		args = append(args, feedID)
	} else {
		q += " AND s.muted = 0"
	}
	if categoryID > 0 {
		q += " AND s.category_id = ?"
		args = append(args, categoryID)
	}
	var n int
	err := s.DB.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}

// SmartViewCounts drives the sidebar badges for Fresh / Starred / Read Later /
// Shared. Each count uses the same semantics the corresponding list view
// shows, so the badge never lies relative to clicking through:
//   - Fresh: unread + summarized + published_at within the last 6 hours
//     (matches isFresh() in ArticleList.svelte; if that horizon ever moves
//     to a config value, update both at once).
//   - Starred: total starred articles.
//   - Later: total saved-for-later articles.
//   - Shared: count of unseen shares received (matches inbox/mention semantics).
type SmartViewCounts struct {
	Fresh   int `json:"fresh"`
	Starred int `json:"starred"`
	Later   int `json:"later"`
	Shared  int `json:"shared"`
	// Unread is the global "All Unread" badge: unread articles within the
	// user's unread window, deduped + gated identically to the All-Unread
	// list. UnreadByCategory is the same, scoped per category id, so a folder
	// badge matches the category list (cross-feed dedup means it can be less
	// than the sum of the per-feed badges inside it).
	Unread           int           `json:"unread"`
	UnreadByCategory map[int64]int `json:"unread_by_category"`
	// PendingSummary: articles in the user's subscribed feeds that the
	// summarizer hasn't touched yet (summary_model NULL or empty). Drains
	// as the poller's summary worker processes them. Drives the
	// "Summarizing N articles" indicator at the bottom of the sidebar.
	// Articles stamped 'disabled' or 'skipped' do NOT count — they've been
	// finalized one way or another.
	PendingSummary int `json:"pending_summary"`
}

// CountSmartViews returns all five counts in a single roundtrip. SQLite
// single-conn pool: bundling the queries doesn't help latency much, but it
// keeps the API surface small.
//
// freshWindow controls the Fresh-view cutoff (unread + summarized +
// published within the window). The caller passes cfg.FreshWindow so the
// EMBER_FRESH_WINDOW env var actually takes effect; a zero or negative
// window falls back to 6h to match the legacy hardcoded value.
func (s *Store) CountSmartViews(ctx context.Context, userID int64, freshWindow time.Duration, unreadCutoff int64, onlySummarized bool) (SmartViewCounts, error) {
	var c SmartViewCounts
	c.UnreadByCategory = map[int64]int{}
	if freshWindow <= 0 {
		freshWindow = 6 * time.Hour
	}
	// Fresh + All-Unread badges go through CountArticles so they share the exact
	// predicate (window, summary gate, cross-feed dedup) of the Fresh and
	// All-Unread lists — the badge can never disagree with the column.
	freshCutoff := s.nowUnix() - int64(freshWindow.Seconds())
	var err error
	if c.Fresh, err = s.CountArticles(ctx, userID, ListArticlesQuery{
		View: "fresh", FreshAfter: freshCutoff, OnlySummarized: onlySummarized,
	}); err != nil {
		return c, fmt.Errorf("count fresh: %w", err)
	}
	if c.Unread, err = s.CountArticles(ctx, userID, ListArticlesQuery{
		View: "unread", FreshAfter: unreadCutoff, OnlySummarized: onlySummarized,
	}); err != nil {
		return c, fmt.Errorf("count unread: %w", err)
	}
	// Per-category unread badges: same predicate, scoped to each category the
	// user has. Cross-feed dedup applies (category lists dedup), so a folder
	// badge can read lower than the sum of its feeds' badges.
	catIDs, err := s.listCategoryIDs(ctx, userID)
	if err != nil {
		return c, fmt.Errorf("count unread by category: %w", err)
	}
	for _, cid := range catIDs {
		n, err := s.CountArticles(ctx, userID, ListArticlesQuery{
			CategoryID: cid, Unread: true, FreshAfter: unreadCutoff, OnlySummarized: onlySummarized,
		})
		if err != nil {
			return c, fmt.Errorf("count unread by category %d: %w", cid, err)
		}
		c.UnreadByCategory[cid] = n
	}
	if err := s.DB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM article_state WHERE user_id = ? AND is_starred = 1`,
		userID).Scan(&c.Starred); err != nil {
		return c, fmt.Errorf("count starred: %w", err)
	}
	if err := s.DB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM article_state WHERE user_id = ? AND is_later = 1`,
		userID).Scan(&c.Later); err != nil {
		return c, fmt.Errorf("count later: %w", err)
	}
	if err := s.DB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM shares WHERE to_user = ? AND seen = 0`,
		userID).Scan(&c.Shared); err != nil {
		return c, fmt.Errorf("count shared: %w", err)
	}
	// PendingSummary: scoped to the user's (non-muted) feeds. Muted feeds
	// are excluded because the user has signalled they don't care; their
	// pending count would just inflate the indicator with no signal.
	if err := s.DB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM articles a
JOIN subscriptions sub ON sub.feed_id = a.feed_id AND sub.user_id = ?
WHERE sub.muted = 0
  AND (a.summary_model IS NULL OR a.summary_model = '')`,
		userID).Scan(&c.PendingSummary); err != nil {
		return c, fmt.Errorf("count pending summary: %w", err)
	}
	return c, nil
}

// listCategoryIDs returns the user's category ids (used to build the per-
// category unread map without pulling full category rows).
func (s *Store) listCategoryIDs(ctx context.Context, userID int64) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id FROM categories WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func scanArticle(row scannable) (models.Article, error) {
	var a models.Article
	err := row.Scan(&a.ID, &a.FeedID, &a.GUID, &a.URL, &a.Title, &a.Author,
		&a.ContentHTML, &a.ContentText, &a.CleanedHTML, &a.Summary, &a.SummaryModel,
		&a.ImageURL, &a.PublishedAt, &a.FetchedAt, &a.ContentHash, &a.Tags)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Article{}, ErrNotFound
	}
	return a, err
}

// ClusterSibling is a peer of the requested article: same cluster_id,
// reachable via a different subscription owned by the same user.
type ClusterSibling struct {
	ArticleID int64  `json:"article_id"`
	FeedID    int64  `json:"feed_id"`
	FeedTitle string `json:"feed_title"`
	URL       string `json:"url"`
	IsRead    bool   `json:"is_read"`
	IsStarred bool   `json:"is_starred"`
}

// ListClusterSiblings returns the other articles in the same cluster as the
// given article that the user is subscribed to via different feeds. Two
// match criteria (OR'd): same cluster_id (canonical URL), or same
// title_fingerprint within a 48h published_at window (catches syndicated
// wire stories under different URLs). The requested article itself is
// excluded. Order is feed title asc for stable UI. Returns an empty slice
// (no error) when the article has neither a cluster_id nor a fingerprint —
// the caller's "Also in N feeds" pill won't have been shown anyway.
//
// Returns ErrNotFound when the article doesn't exist or the user can't see
// it (no subscription to its feed, or the feed is muted).
func (s *Store) ListClusterSiblings(ctx context.Context, userID, articleID int64) ([]ClusterSibling, error) {
	// Grab the article's cluster_id, title_fingerprint, and published_at —
	// the three inputs the sibling query needs.
	var cid, fp string
	var pubAt int64
	err := s.DB.QueryRowContext(ctx, `
		SELECT IFNULL(a.cluster_id,''), IFNULL(a.title_fingerprint,''), IFNULL(a.published_at,0)
		FROM articles a
		JOIN subscriptions sub ON sub.feed_id = a.feed_id AND sub.user_id = ? AND sub.muted = 0
		WHERE a.id = ?`, userID, articleID).Scan(&cid, &fp, &pubAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("list cluster siblings: lookup: %w", err)
	}
	if cid == "" && fp == "" {
		return []ClusterSibling{}, nil
	}

	rows, err := s.DB.QueryContext(ctx, `
		SELECT a.id, a.feed_id, IFNULL(f.title,''), IFNULL(a.url,''),
		       IFNULL(st.is_read,0), IFNULL(st.is_starred,0)
		FROM articles a
		JOIN feeds f ON f.id = a.feed_id
		JOIN subscriptions sub ON sub.feed_id = a.feed_id AND sub.user_id = ? AND sub.muted = 0
		LEFT JOIN article_state st ON st.article_id = a.id AND st.user_id = ?
		WHERE a.id <> ? AND (
			(a.cluster_id = ? AND a.cluster_id <> '')
			OR (
				a.title_fingerprint = ? AND a.title_fingerprint <> ''
				AND ABS(IFNULL(a.published_at,0) - ?) < 172800
			)
		)
		ORDER BY f.title ASC, a.id ASC`,
		userID, userID, articleID, cid, fp, pubAt)
	if err != nil {
		return nil, fmt.Errorf("list cluster siblings: query: %w", err)
	}
	defer rows.Close()

	var out []ClusterSibling
	for rows.Next() {
		var s ClusterSibling
		var ir, is int
		if err := rows.Scan(&s.ArticleID, &s.FeedID, &s.FeedTitle, &s.URL, &ir, &is); err != nil {
			return nil, fmt.Errorf("list cluster siblings: scan: %w", err)
		}
		s.IsRead = ir != 0
		s.IsStarred = is != 0
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list cluster siblings: iterate: %w", err)
	}
	if out == nil {
		out = []ClusterSibling{}
	}
	return out, nil
}
