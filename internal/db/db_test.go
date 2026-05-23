package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpen_MigrationsAndPragmas(t *testing.T) {
	dbh := OpenTest(t)
	ctx := context.Background()

	// All core tables exist.
	wantTables := []string{
		"users", "sessions", "feeds", "categories", "subscriptions",
		"articles", "article_state", "boards", "board_articles",
		"filters", "shares", "articles_fts",
	}
	for _, name := range wantTables {
		var got string
		err := dbh.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE name = ?`, name).Scan(&got)
		if err != nil {
			t.Errorf("expected table %q: %v", name, err)
		}
	}

	// FTS triggers exist.
	for _, trig := range []string{"articles_ai", "articles_ad", "articles_au"} {
		var got string
		err := dbh.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='trigger' AND name = ?`, trig).Scan(&got)
		if err != nil {
			t.Errorf("expected trigger %q: %v", trig, err)
		}
	}

	// PRAGMAs applied.
	var journal string
	if err := dbh.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err != nil {
		t.Fatalf("pragma journal_mode: %v", err)
	}
	if journal != "wal" {
		t.Errorf("journal_mode = %q, want wal", journal)
	}
	var fk int
	if err := dbh.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("pragma foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}
}

func TestMigrate_UpDownIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "mig.db")
	dbh, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer dbh.Close()

	// Up already applied by Open; running again should be a no-op.
	if err := Migrate(ctx, dbh); err != nil {
		t.Fatalf("re-migrate up: %v", err)
	}

	// Reset all the way back and re-apply.
	if err := MigrateReset(ctx, dbh); err != nil {
		t.Fatalf("reset: %v", err)
	}

	// After reset, articles table should not exist.
	var name string
	err = dbh.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE name='articles'`).Scan(&name)
	if err == nil {
		t.Errorf("articles table still exists after reset")
	}

	// Bring it back up.
	if err := Migrate(ctx, dbh); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	if err := dbh.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE name='articles'`).Scan(&name); err != nil {
		t.Fatalf("articles missing after re-up: %v", err)
	}
}

func TestOpen_InvalidPath(t *testing.T) {
	ctx := context.Background()
	// A directory that doesn't exist forces sqlite to fail when it tries to
	// create the file.
	_, err := Open(ctx, "/no/such/dir/ember.db")
	if err == nil {
		t.Fatal("expected error for unwritable path")
	}
}
