package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// SetRead marks an article (or many articles) read/unread for the user.
func (s *Store) SetRead(ctx context.Context, userID int64, articleIDs []int64, read bool) error {
	return s.setStateFlag(ctx, userID, articleIDs, "is_read", "read_at", read)
}

// SetStarred toggles the star flag for a single article.
func (s *Store) SetStarred(ctx context.Context, userID, articleID int64, starred bool) error {
	return s.setStateFlag(ctx, userID, []int64{articleID}, "is_starred", "starred_at", starred)
}

// SetLater toggles the read-later flag for a single article.
func (s *Store) SetLater(ctx context.Context, userID, articleID int64, later bool) error {
	return s.setStateFlag(ctx, userID, []int64{articleID}, "is_later", "", later)
}

// MarkBoardRead marks every article on the board as read for the user. The
// board must belong to the user (otherwise this is a no-op).
func (s *Store) MarkBoardRead(ctx context.Context, userID, boardID int64) (int64, error) {
	q := `
INSERT INTO article_state (user_id, article_id, is_read, read_at)
SELECT ?, a.id, 1, ?
FROM articles a
JOIN board_articles ba ON ba.article_id = a.id
JOIN boards b ON b.id = ba.board_id AND b.user_id = ? AND b.id = ?
LEFT JOIN article_state st ON st.article_id = a.id AND st.user_id = ?
WHERE IFNULL(st.is_read,0) = 0
ON CONFLICT(user_id, article_id) DO UPDATE SET is_read = 1, read_at = excluded.read_at
WHERE article_state.is_read = 0`
	res, err := s.DB.ExecContext(ctx, q, userID, s.nowUnix(), userID, boardID, userID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// MarkAllRead marks every article in scope as read for the user. Pass 0 to a
// scope filter to omit it (e.g. category=0, feed=0 means mark-all-read globally).
// freshAfter limits to articles published at or after that time (used for the
// "fresh"/"today" view).
func (s *Store) MarkAllRead(ctx context.Context, userID, feedID, categoryID, freshAfter int64) (int64, error) {
	// Insert/update state rows for every article in scope that isn't already
	// marked read. UPSERT.
	q := `
INSERT INTO article_state (user_id, article_id, is_read, read_at)
SELECT ?, a.id, 1, ?
FROM articles a
JOIN subscriptions s ON s.feed_id = a.feed_id AND s.user_id = ?
LEFT JOIN article_state st ON st.article_id = a.id AND st.user_id = ?
WHERE IFNULL(st.is_read,0) = 0`
	now := s.nowUnix()
	args := []any{userID, now, userID, userID}
	if feedID > 0 {
		q += " AND a.feed_id = ?"
		args = append(args, feedID)
	}
	if categoryID > 0 {
		q += " AND s.category_id = ?"
		args = append(args, categoryID)
	}
	if freshAfter > 0 {
		q += " AND IFNULL(a.published_at,0) >= ?"
		args = append(args, freshAfter)
	}
	q += `
ON CONFLICT(user_id, article_id) DO UPDATE SET is_read = 1, read_at = excluded.read_at
WHERE article_state.is_read = 0`
	res, err := s.DB.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// GetState returns the user's state row for an article. Missing rows return a
// zero ArticleState (all flags false), not ErrNotFound.
func (s *Store) GetState(ctx context.Context, userID, articleID int64) (struct {
	IsRead    bool
	IsStarred bool
	IsLater   bool
	ReadAt    int64
	StarredAt int64
}, error,
) {
	var out struct {
		IsRead    bool
		IsStarred bool
		IsLater   bool
		ReadAt    int64
		StarredAt int64
	}
	var ir, is, il int
	var ra, sa int64
	err := s.DB.QueryRowContext(ctx, `
		SELECT is_read, is_starred, is_later, IFNULL(read_at,0), IFNULL(starred_at,0)
		FROM article_state WHERE user_id = ? AND article_id = ?`,
		userID, articleID).Scan(&ir, &is, &il, &ra, &sa)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.IsRead, out.IsStarred, out.IsLater = ir == 1, is == 1, il == 1
	out.ReadAt, out.StarredAt = ra, sa
	return out, nil
}

func (s *Store) setStateFlag(ctx context.Context, userID int64, articleIDs []int64, flagCol, timeCol string, on bool) error {
	if len(articleIDs) == 0 {
		return nil
	}
	now := s.nowUnix()
	flagVal := 0
	if on {
		flagVal = 1
	}
	placeholders := make([]string, len(articleIDs))
	args := []any{}
	for i, id := range articleIDs {
		placeholders[i] = "(?, ?, ?, ?)"
		args = append(args, userID, id, flagVal, now)
	}

	// Build the SET clause: always update flagCol; update timeCol when turning on.
	setExpr := flagCol + " = excluded." + flagCol
	if timeCol != "" {
		setExpr += ", " + timeCol + " = CASE WHEN excluded." + flagCol +
			" = 1 THEN excluded." + timeCol + " ELSE article_state." + timeCol + " END"
	}

	q := fmt.Sprintf(`
INSERT INTO article_state (user_id, article_id, %s, %s)
VALUES %s
ON CONFLICT(user_id, article_id) DO UPDATE SET %s`,
		flagCol, ifEmpty(timeCol, "read_at"), strings.Join(placeholders, ","), setExpr)

	// When no timestamp column applies (is_later), the column-list above still
	// needs to insert SOMETHING for the placeholder; use read_at (it stays
	// NULL via the default ON CONFLICT path).
	_, err := s.DB.ExecContext(ctx, q, args...)
	return err
}

func ifEmpty(a, b string) string {
	if a == "" {
		return b
	}
	return a
}
