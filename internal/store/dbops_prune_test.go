package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// touch writes a file with a specific mtime so prune ordering is deterministic.
func touch(t *testing.T, path string, age time.Duration) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// The backup directory is an admin-configurable absolute path. If it is ever
// pointed at the directory that holds the live database, prune must not be
// able to delete it. The live DB is written continuously so it is usually the
// newest file — but on an idle instance it ages past the retained backups.
func TestPruneBackups_NeverDeletesTheLiveDatabase(t *testing.T) {
	s := NewTest(t)
	dir := t.TempDir()

	live := filepath.Join(dir, "ember.db")
	touch(t, live, 30*24*time.Hour) // idle instance: older than every backup
	for i, name := range []string{
		"ember-2026-07-27-100000.db",
		"ember-2026-07-26-100000.db",
		"ember-2026-07-25-100000.db",
	} {
		touch(t, filepath.Join(dir, name), time.Duration(i+1)*time.Hour)
	}

	if _, err := s.PruneBackups(dir, 3); err != nil {
		t.Fatalf("PruneBackups: %v", err)
	}
	if !has(names(t, dir), "ember.db") {
		t.Fatal("PruneBackups DELETED THE LIVE DATABASE (ember.db) — data loss")
	}
}

// Only ember's own backup files are prune candidates; unrelated files an admin
// keeps in the same directory must survive.
func TestPruneBackups_OnlyTouchesEmberBackups(t *testing.T) {
	s := NewTest(t)
	dir := t.TempDir()

	touch(t, filepath.Join(dir, "someone-elses-archive.db"), 40*24*time.Hour)
	touch(t, filepath.Join(dir, "notes.txt"), 40*24*time.Hour)
	for i, name := range []string{
		"ember-2026-07-27-100000.db",
		"ember-2026-07-26-100000.db",
		"ember-2026-07-25-100000.db",
	} {
		touch(t, filepath.Join(dir, name), time.Duration(i+1)*time.Hour)
	}

	deleted, err := s.PruneBackups(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Errorf("deleted %d, want 2 (the two older ember backups)", deleted)
	}
	left := names(t, dir)
	for _, keep := range []string{"someone-elses-archive.db", "notes.txt", "ember-2026-07-27-100000.db"} {
		if !has(left, keep) {
			t.Errorf("%s was deleted; remaining: %v", keep, left)
		}
	}
}

// keep <= 0 means "unset / misconfigured" and must delete NOTHING — the
// opposite reading would wipe every backup the first time it ran.
func TestPruneBackups_ZeroKeepDeletesNothing(t *testing.T) {
	s := NewTest(t)
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "ember-2026-07-27-100000.db"), time.Hour)
	for _, keep := range []int{0, -1} {
		n, err := s.PruneBackups(dir, keep)
		if err != nil || n != 0 {
			t.Errorf("PruneBackups(keep=%d) = %d, %v; want 0, nil", keep, n, err)
		}
		if len(names(t, dir)) != 1 {
			t.Fatalf("keep=%d removed files: %v", keep, names(t, dir))
		}
	}
}

// Same protections for the OPML export directory.
func TestPruneExports_OnlyTouchesEmberExports(t *testing.T) {
	s := NewTest(t)
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "subscriptions-from-elsewhere.opml"), 40*24*time.Hour)
	for i, name := range []string{
		"ember-2026-07-27-100000.opml",
		"ember-2026-07-26-100000.opml",
	} {
		touch(t, filepath.Join(dir, name), time.Duration(i+1)*time.Hour)
	}
	if _, err := s.PruneExports(dir, 1); err != nil {
		t.Fatal(err)
	}
	if !has(names(t, dir), "subscriptions-from-elsewhere.opml") {
		t.Errorf("prune deleted a non-ember OPML file; remaining: %v", names(t, dir))
	}
}

func TestListBackups_SortsNewestFirstAndIgnoresNoise(t *testing.T) {
	s := NewTest(t)
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "ember-2026-07-25-100000.db"), 3*time.Hour)
	touch(t, filepath.Join(dir, "ember-2026-07-27-100000.db"), 1*time.Hour)
	touch(t, filepath.Join(dir, "ember-2026-07-26-100000.db"), 2*time.Hour)
	touch(t, filepath.Join(dir, "readme.txt"), time.Hour)
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o750); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListBackups(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("listed %d backups, want 3: %+v", len(list), list)
	}
	for i := 1; i < len(list); i++ {
		if list[i-1].CreatedAt < list[i].CreatedAt {
			t.Errorf("not sorted newest-first: %+v", list)
		}
	}
}

func TestListBackups_MissingDirIsNotAnError(t *testing.T) {
	s := NewTest(t)
	list, err := s.ListBackups(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil || list != nil {
		t.Errorf("ListBackups(missing) = %v, %v; want nil, nil", list, err)
	}
}

// The delete endpoints take a caller-supplied name; traversal and wrong-type
// targets must be refused.
func TestDeleteFileInDir_RejectsTraversalAndWrongType(t *testing.T) {
	s := NewTest(t)
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "victim.db")
	touch(t, outside, time.Hour)
	touch(t, filepath.Join(dir, "ember-2026-07-27-100000.db"), time.Hour)
	touch(t, filepath.Join(dir, "keep.opml"), time.Hour)

	for _, name := range []string{
		"", "../victim.db", "../../victim.db", "/etc/passwd",
		"subdir/x.db", "keep.opml", "ember.db-wal", "nope.db",
	} {
		if err := s.DeleteBackup(dir, name); err == nil {
			t.Errorf("DeleteBackup(%q) was accepted", name)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("a file outside the directory was removed: %v", err)
	}
	if !has(names(t, dir), "keep.opml") {
		t.Error("DeleteBackup removed a .opml file")
	}
	// The legitimate case still works.
	if err := s.DeleteBackup(dir, "ember-2026-07-27-100000.db"); err != nil {
		t.Errorf("legitimate delete failed: %v", err)
	}
}
