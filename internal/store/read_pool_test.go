package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/db"
	"github.com/brandonhon/ember/internal/models"
)

// A Store with no ReadDB must behave exactly as before — reader() falls back
// to the write handle, so tests and any deployment where the read pool failed
// to open are unaffected.
func TestReader_FallsBackToWriteHandle(t *testing.T) {
	// A Store built without a read pool — the shape main uses when OpenRead
	// fails, and what any external caller of New() gets.
	s := New(db.OpenTest(t))
	if s.ReadDB != nil {
		t.Fatal("New() must not configure a read pool")
	}
	if s.reader() != s.DB {
		t.Error("reader() must fall back to DB when ReadDB is nil")
	}
	// And it still works end to end.
	ctx := context.Background()
	u, err := s.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListFeedsForUser(ctx, u.ID, 0, false); err != nil {
		t.Errorf("routed method failed without a read pool: %v", err)
	}
}

// openBoth returns a Store wired to a write handle and a read pool over the
// same on-disk database.
func openBoth(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pool.db")
	w, err := db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := db.OpenRead(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	s := New(w)
	s.ReadDB = r
	return s
}

// The safety net: db.OpenRead sets query_only, so SQLite itself refuses a
// write on the read pool. A store method mistakenly routed to reader() fails
// immediately and visibly instead of silently reintroducing write contention.
func TestReadPool_RejectsWrites(t *testing.T) {
	s := openBoth(t)
	ctx := context.Background()
	if _, err := s.ReadDB.ExecContext(ctx,
		`INSERT INTO feeds (url, title, fetch_interval, error_count, created_at) VALUES ('x','y',1800,0,1)`); err == nil {
		t.Fatal("read pool accepted a write — query_only is not in effect")
	}
	// The write handle still works.
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO feeds (url, title, fetch_interval, error_count, created_at) VALUES ('x','y',1800,0,1)`); err != nil {
		t.Fatalf("write handle rejected a write: %v", err)
	}
}

// The routed methods must return identical results through either handle —
// the split is a plumbing change, not a behaviour change.
func TestReadPool_SameResultsAsWriteHandle(t *testing.T) {
	s := openBoth(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := s.UpsertFeed(ctx, models.Feed{URL: "https://f.test/rss", Title: "F"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f.ID}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	for i := range 5 {
		if _, _, err := s.UpsertArticle(ctx, models.Article{
			FeedID: f.ID, GUID: string(rune('a' + i)), Title: "T",
			ContentHash: string(rune('h' + i)), PublishedAt: now - int64(i*60),
			SummaryModel: "noop",
		}); err != nil {
			t.Fatal(err)
		}
	}
	cutoff := now - 86400
	q := ListArticlesQuery{View: "today", FreshAfter: cutoff, Limit: 50}

	viaRead, err := s.ListArticles(ctx, u.ID, q)
	if err != nil {
		t.Fatal(err)
	}
	// Force the same call through the write handle for comparison.
	saved := s.ReadDB
	s.ReadDB = nil
	viaWrite, err := s.ListArticles(ctx, u.ID, q)
	s.ReadDB = saved
	if err != nil {
		t.Fatal(err)
	}
	if len(viaRead) != len(viaWrite) || len(viaRead) != 5 {
		t.Fatalf("read pool returned %d rows, write handle %d, want 5", len(viaRead), len(viaWrite))
	}
	for i := range viaRead {
		if viaRead[i].ID != viaWrite[i].ID {
			t.Errorf("row %d differs: read=%d write=%d", i, viaRead[i].ID, viaWrite[i].ID)
		}
	}

	// The count queries agree too.
	n, err := s.CountArticles(ctx, u.ID, q)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Errorf("CountArticles via read pool = %d, want 5", n)
	}
}

// A write on the write handle must be visible to the read pool immediately —
// WAL readers see committed data, so there is no stale-read window that would
// make a freshly-added feed vanish from the list.
func TestReadPool_SeesCommittedWritesImmediately(t *testing.T) {
	s := openBoth(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := s.UpsertFeed(ctx, models.Feed{URL: "https://f.test/rss", Title: "F"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f.ID}); err != nil {
		t.Fatal(err)
	}
	// ListFeedsForUser is one of the routed methods; it must see the
	// subscription that was just committed on the other handle.
	feeds, err := s.ListFeedsForUser(ctx, u.ID, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 1 {
		t.Fatalf("read pool saw %d feeds after a committed write, want 1", len(feeds))
	}
}

// The read pool must actually apply its own smaller page cache — four
// connections at the shared 64 MiB would take the ceiling from 64 MiB to
// 320 MiB, which is not acceptable on the boxes ember targets.
func TestReadPool_UsesSmallerPageCache(t *testing.T) {
	s := openBoth(t)
	ctx := context.Background()
	var readCache, writeCache int
	if err := s.ReadDB.QueryRowContext(ctx, "PRAGMA cache_size").Scan(&readCache); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRowContext(ctx, "PRAGMA cache_size").Scan(&writeCache); err != nil {
		t.Fatal(err)
	}
	if readCache != -16384 {
		t.Errorf("read pool cache_size = %d, want -16384 (16 MiB)", readCache)
	}
	if writeCache != -65536 {
		t.Errorf("write handle cache_size = %d, want -65536 (unchanged)", writeCache)
	}
	// query_only must still be in effect alongside the cache override.
	var qo int
	if err := s.ReadDB.QueryRowContext(ctx, "PRAGMA query_only").Scan(&qo); err != nil {
		t.Fatal(err)
	}
	if qo != 1 {
		t.Error("query_only lost when the cache_size override was added")
	}
}

// EDGE CASE 1: read-after-write across the two handles.
// The SPA marks an article read (write) then immediately polls the counts
// (routed to the read pool). If the reader could serve a stale WAL snapshot
// the badge would lag behind the click. Hammered, because a staleness window
// would be intermittent.
func TestReadPool_ReadAfterWriteIsImmediatelyVisible(t *testing.T) {
	s := openBoth(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "x"})
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://f.test/rss", Title: "F"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f.ID})

	now := time.Now().Unix()
	var ids []int64
	for i := range 30 {
		a, _, err := s.UpsertArticle(ctx, models.Article{
			FeedID: f.ID, GUID: fmt.Sprintf("g%d", i), Title: "T",
			ContentHash: fmt.Sprintf("h%d", i), PublishedAt: now - int64(i*60),
			SummaryModel: "noop",
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, a.ID)
	}
	q := ListArticlesQuery{View: "unread", FreshAfter: now - 86400}

	// After each write, the very next read must reflect it.
	for i, id := range ids {
		if err := s.SetRead(ctx, u.ID, []int64{id}, true); err != nil {
			t.Fatal(err)
		}
		got, err := s.CountArticles(ctx, u.ID, q)
		if err != nil {
			t.Fatal(err)
		}
		if want := len(ids) - (i + 1); got != want {
			t.Fatalf("after marking %d read, read pool reported %d unread, want %d — STALE SNAPSHOT", i+1, got, want)
		}
	}
}

// EDGE CASE 2: FTS5 MATCH under query_only.
// Search is one of the routed methods. If FTS5 needed to touch a shadow table
// the read pool would refuse it and search would break entirely — a total
// feature outage, not a slowdown.
func TestReadPool_FTSSearchWorksUnderQueryOnly(t *testing.T) {
	s := openBoth(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "x"})
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://f.test/rss", Title: "F"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f.ID})
	now := time.Now().Unix()
	if _, _, err := s.UpsertArticle(ctx, models.Article{
		FeedID: f.ID, GUID: "g1", Title: "Quantum computing breakthrough",
		ContentText: "researchers announced a quantum milestone",
		ContentHash: "h1", PublishedAt: now, SummaryModel: "noop",
	}); err != nil {
		t.Fatal(err)
	}
	hits, err := s.Search(ctx, u.ID, "quantum", 25, 0, 0)
	if err != nil {
		t.Fatalf("FTS search through the read pool failed: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("search returned %d hits, want 1", len(hits))
	}
}

// EDGE CASE 3: the starter-pack remove flow reads ListFeedsForUser, writes
// (unsubscribe), then reads it AGAIN to decide whether the category is now
// empty. The second read crosses handles and must see the unsubscribes.
func TestReadPool_ListFeedsForUserSeesInterleavedWrites(t *testing.T) {
	s := openBoth(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "x"})
	c, _ := s.CreateCategory(ctx, models.Category{UserID: u.ID, Name: "Tech"})

	var subIDs []int64
	for i := range 3 {
		f, _ := s.UpsertFeed(ctx, models.Feed{URL: fmt.Sprintf("https://f%d.test/rss", i), Title: "F"})
		sub, err := s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f.ID, CategoryID: &c.ID})
		if err != nil {
			t.Fatal(err)
		}
		subIDs = append(subIDs, sub.ID)
	}
	if got, _ := s.ListFeedsForUser(ctx, u.ID, 0, false); len(got) != 3 {
		t.Fatalf("setup: %d feeds, want 3", len(got))
	}
	// Unsubscribe one at a time; each subsequent read must reflect it.
	for i, id := range subIDs {
		if err := s.Unsubscribe(ctx, u.ID, id); err != nil {
			t.Fatal(err)
		}
		got, err := s.ListFeedsForUser(ctx, u.ID, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		if want := len(subIDs) - (i + 1); len(got) != want {
			t.Fatalf("after %d unsubscribes the read pool saw %d feeds, want %d — STALE", i+1, len(got), want)
		}
	}
}
