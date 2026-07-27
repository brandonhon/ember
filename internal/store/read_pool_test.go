package store

import (
	"context"
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
	s := NewTest(t)
	if s.ReadDB != nil {
		t.Fatal("NewTest should not configure a read pool")
	}
	if s.reader() != s.DB {
		t.Error("reader() must fall back to DB when ReadDB is nil")
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
