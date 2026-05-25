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

// Pragmas applied to every connection.
//   - journal_mode=WAL: concurrent readers + one writer
//   - foreign_keys=ON: enforce referential integrity
//   - busy_timeout=5s: wait instead of SQLITE_BUSY on contention
//   - synchronous=NORMAL: safe with WAL, ~2x faster than FULL for our writes
//   - temp_store=MEMORY: temp tables in RAM
//   - cache_size=-65536: 64 MiB page cache (default is 2 MiB — too small for
//     our workload of nested article queries with dedup joins)
//   - mmap_size=268435456: 256 MiB memory-mapped IO for read-heavy paths
const pragmas = `
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;
PRAGMA synchronous=NORMAL;
PRAGMA temp_store=MEMORY;
PRAGMA cache_size=-65536;
PRAGMA mmap_size=268435456;
`

// Open opens the SQLite database at path, applies PRAGMAs, and runs all
// pending migrations. Returns the database handle.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"
	dbh, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	// WAL mode supports concurrent readers + one writer. modernc.org/sqlite
	// serializes writes via a mutex internally, so MaxOpenConns can safely be
	// >1; reads then run in parallel which matters once the SPA is polling
	// + the poller is ingesting + admins are looking at feed lists.
	dbh.SetMaxOpenConns(8)
	dbh.SetMaxIdleConns(4)
	dbh.SetConnMaxIdleTime(5 * time.Minute)
	if _, err := dbh.ExecContext(ctx, pragmas); err != nil {
		dbh.Close()
		return nil, fmt.Errorf("apply pragmas: %w", err)
	}
	if err := Migrate(ctx, dbh); err != nil {
		dbh.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	// Refresh query planner stats so range/keyset scans hit good plans even
	// on a long-running DB that's drifted from its initial ANALYZE.
	if _, err := dbh.ExecContext(ctx, "PRAGMA optimize;"); err != nil {
		// Non-fatal — log via caller's logger isn't available here.
		_ = err
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
