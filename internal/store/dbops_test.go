package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneExports_KeepsNewestN(t *testing.T) {
	s := NewTest(t)
	dir := t.TempDir()

	// Create 5 .opml files with explicit, increasing mtimes so newest-first
	// ordering is deterministic.
	now := time.Now()
	names := []string{"a.opml", "b.opml", "c.opml", "d.opml", "e.opml"}
	for i, n := range names {
		path := filepath.Join(dir, n)
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := now.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(path, mt, mt); err != nil {
			t.Fatal(err)
		}
	}

	// Also drop a non-.opml file. It must be ignored.
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	deleted, err := s.PruneExports(dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 3 {
		t.Errorf("deleted %d, want 3", deleted)
	}

	left, err := s.ListExports(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 2 {
		t.Fatalf("len(left) = %d, want 2", len(left))
	}
	// Newest two are e.opml and d.opml (newest first).
	if filepath.Base(left[0].Path) != "e.opml" || filepath.Base(left[1].Path) != "d.opml" {
		t.Errorf("kept wrong files: %s, %s", left[0].Path, left[1].Path)
	}
	// Non-.opml sibling is untouched.
	if _, err := os.Stat(filepath.Join(dir, "ignore.txt")); err != nil {
		t.Errorf("non-opml sibling deleted: %v", err)
	}
}

func TestPruneExports_NoopWhenAtOrBelowKeep(t *testing.T) {
	s := NewTest(t)
	dir := t.TempDir()
	for _, n := range []string{"a.opml", "b.opml"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := s.PruneExports(dir, 5)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Errorf("deleted %d, want 0", deleted)
	}
}

func TestPruneExports_MissingDirIsOK(t *testing.T) {
	s := NewTest(t)
	deleted, err := s.PruneExports(filepath.Join(t.TempDir(), "does-not-exist"), 3)
	if err != nil {
		t.Errorf("missing dir should not error, got %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted %d, want 0", deleted)
	}
}

func TestPruneExports_NonPositiveKeep(t *testing.T) {
	s := NewTest(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.opml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := s.PruneExports(dir, 0)
	if err != nil || n != 0 {
		t.Errorf("keep=0: n=%d err=%v, want n=0 err=nil", n, err)
	}
}

func TestDeleteBackup_RejectsTraversalAndBadNames(t *testing.T) {
	s := NewTest(t)
	dir := t.TempDir()

	// Happy path: a real backup is deleted.
	good := filepath.Join(dir, "good.db")
	if err := os.WriteFile(good, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBackup(dir, "good.db"); err != nil {
		t.Fatalf("delete good.db: %v", err)
	}
	if _, err := os.Stat(good); !os.IsNotExist(err) {
		t.Errorf("good.db still present, stat err = %v", err)
	}

	// A file the deleter must never reach: a .db sibling outside dir.
	outside := filepath.Join(t.TempDir(), "secret.db")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A wrong-extension file inside dir, which must also be untouchable.
	keep := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(keep, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{
		"../secret.db",                   // parent traversal
		"sub/x.db",                       // separator
		filepath.Join("..", "secret.db"), // os-native traversal
		"keep.txt",                       // wrong extension
		"missing.db",                     // absent
		"",                               // empty
	} {
		if err := s.DeleteBackup(dir, name); !errors.Is(err, ErrNotFound) {
			t.Errorf("DeleteBackup(%q) = %v, want ErrNotFound", name, err)
		}
	}

	// The off-limits files are untouched.
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("outside secret.db was disturbed: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("keep.txt was disturbed: %v", err)
	}
}

// TestCleanup_FTSOptimize ensures that the FTS5 optimize step appended to
// Cleanup runs successfully against a real FTS index after article deletes.
func TestCleanup_FTSOptimize(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	_, feedID := seedUserAndFeed(t, s, "alice")

	// Insert several articles with old fetched_at so they qualify for cleanup.
	old := time.Now().Add(-200 * 24 * time.Hour).Unix()
	for i := 0; i < 5; i++ {
		art := mkArticle(feedID, "g"+string(rune('a'+i)), "title", "h"+string(rune('a'+i)), 0)
		got, _, err := s.UpsertArticle(ctx, art)
		if err != nil {
			t.Fatal(err)
		}
		// Force fetched_at into the past so Cleanup matches the row.
		if _, err := s.DB.ExecContext(ctx,
			`UPDATE articles SET fetched_at = ? WHERE id = ?`, old, got.ID); err != nil {
			t.Fatal(err)
		}
	}

	stats, err := s.Cleanup(ctx, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("cleanup with fts optimize failed: %v", err)
	}
	if stats.ArticlesDeleted != 5 {
		t.Errorf("ArticlesDeleted = %d, want 5", stats.ArticlesDeleted)
	}
}
