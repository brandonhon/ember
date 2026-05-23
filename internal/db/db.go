// Package db opens the SQLite database, applies PRAGMAs, and runs migrations.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	_ "modernc.org/sqlite" // sqlite driver (pure Go, CGO-free)
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Pragmas applied to every connection. WAL + foreign keys + busy timeout +
// reasonable cache size.
const pragmas = `
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;
PRAGMA synchronous=NORMAL;
PRAGMA temp_store=MEMORY;
`

// Open opens the SQLite database at path, applies PRAGMAs, and runs all
// pending migrations. Returns the database handle.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"
	dbh, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	dbh.SetMaxOpenConns(1) // SQLite single-writer; readers OK but keep it simple
	if _, err := dbh.ExecContext(ctx, pragmas); err != nil {
		dbh.Close()
		return nil, fmt.Errorf("apply pragmas: %w", err)
	}
	if err := Migrate(ctx, dbh); err != nil {
		dbh.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return dbh, nil
}

// Migrate runs all pending up migrations from the embedded migrations FS.
func Migrate(ctx context.Context, dbh *sql.DB) error {
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, dbh, "migrations"); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

// MigrateDown rolls back the most recent migration. Used in tests.
func MigrateDown(ctx context.Context, dbh *sql.DB) error {
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}
	return goose.DownContext(ctx, dbh, "migrations")
}

// MigrateReset rolls back every applied migration. Used in tests.
func MigrateReset(ctx context.Context, dbh *sql.DB) error {
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}
	return goose.ResetContext(ctx, dbh, "migrations")
}

// OpenTest returns an isolated, migrated SQLite database backed by a temporary
// file. The database is automatically closed and removed when the test ends.
func OpenTest(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ember-test.db")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dbh, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("OpenTest: %v", err)
	}
	t.Cleanup(func() { _ = dbh.Close() })
	return dbh
}
