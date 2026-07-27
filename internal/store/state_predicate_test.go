package store

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The starred/later predicates are written bare (`st.is_starred = 1`) rather
// than IFNULL-wrapped so SQLite can drive them from idx_state_user_star. This
// pins the equivalence that makes that safe: an article with NO article_state
// row must be excluded from starred/later either way.
func TestCountStarredLater_MatchesIFNULLSemantics(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	userID, feedID := seedUserAndFeed(t, s, "alice")

	now := time.Now().Unix()
	var ids []int64
	for i := range 6 {
		a, _, err := s.UpsertArticle(ctx, mkArticle(feedID, "g"+strconv.Itoa(i), "T"+strconv.Itoa(i), "h"+strconv.Itoa(i), now-int64(i*60)))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, a.ID)
	}
	// Two starred, one saved-for-later; the rest have NO article_state row at
	// all, which is the case the IFNULL used to paper over.
	for _, id := range ids[:2] {
		if err := s.SetStarred(ctx, userID, id, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetLater(ctx, userID, ids[2], true); err != nil {
		t.Fatal(err)
	}
	// An explicit un-star writes a state row with is_starred=0 — distinct from
	// "no row", and it must not be counted either.
	if err := s.SetStarred(ctx, userID, ids[3], true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStarred(ctx, userID, ids[3], false); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		view, ifnull string
		want         int
	}{
		{"starred", "IFNULL(st.is_starred,0) = 1", 2},
		{"later", "IFNULL(st.is_later,0) = 1", 1},
	} {
		t.Run(tc.view, func(t *testing.T) {
			got, err := s.CountArticles(ctx, userID, ListArticlesQuery{View: tc.view})
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("CountArticles(%s) = %d, want %d", tc.view, got, tc.want)
			}
			// Run the IFNULL form of the same query and require an identical
			// answer — this is the equivalence the optimization rests on.
			from, where, args := s.buildArticleFilter(userID, ListArticlesQuery{View: tc.view}, false)
			bare := "st.is_" + map[string]string{"starred": "starred", "later": "later"}[tc.view] + " = 1"
			legacy := strings.Replace(where, bare, tc.ifnull, 1)
			if legacy == where {
				t.Fatalf("bare predicate %q not found in WHERE — predicate changed shape:\n%s", bare, where)
			}
			var viaIFNULL int
			if err := s.DB.QueryRowContext(ctx,
				"SELECT COUNT(*) "+from+" "+legacy, args...).Scan(&viaIFNULL); err != nil {
				t.Fatal(err)
			}
			if viaIFNULL != got {
				t.Errorf("bare predicate = %d but IFNULL form = %d — NOT equivalent", got, viaIFNULL)
			}
		})
	}
}

// The mirror of the above: Unread MUST stay IFNULL-wrapped. An article nobody
// has touched has no article_state row, and it is unread. Rewriting it to
// `st.is_read = 0` would drop every never-touched article from every unread
// count — the exact bug the comment in buildArticleFilter warns about.
func TestCountUnread_CountsArticlesWithNoStateRow(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	userID, feedID := seedUserAndFeed(t, s, "alice")

	now := time.Now().Unix()
	var ids []int64
	for i := range 5 {
		a, _, err := s.UpsertArticle(ctx, mkArticle(feedID, "g"+strconv.Itoa(i), "T"+strconv.Itoa(i), "h"+strconv.Itoa(i), now-int64(i*60)))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, a.ID)
	}
	// Nothing read yet and NO state rows exist: all 5 are unread.
	got, err := s.CountArticles(ctx, userID, ListArticlesQuery{View: "unread", FreshAfter: now - 3600})
	if err != nil {
		t.Fatal(err)
	}
	if got != 5 {
		t.Fatalf("unread with no state rows = %d, want 5 (a missing row means unread)", got)
	}

	if err := s.SetRead(ctx, userID, ids[:2], true); err != nil {
		t.Fatal(err)
	}
	got, err = s.CountArticles(ctx, userID, ListArticlesQuery{View: "unread", FreshAfter: now - 3600})
	if err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Errorf("unread after reading 2 = %d, want 3", got)
	}
}
