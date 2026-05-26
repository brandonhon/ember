// Package db opens the SQLite database, applies PRAGMAs, and runs migrations.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
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
	// All tuning pragmas live in the DSN so they apply per-connection. The
	// later ExecContext(pragmas) only hits one connection in the pool, so
	// without DSN-side pragmas the rest of the pool would run without WAL /
	// cache / mmap tuning — which is why an earlier attempt to raise the
	// pool size caused SQLITE_BUSY storms.
	dsn := path +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(ON)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=temp_store(MEMORY)" +
		"&_pragma=cache_size(-65536)" +
		"&_pragma=mmap_size(268435456)"
	dbh, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	// Serialize through a single connection. SQLite has one writer; with
	// multiple Go conns the poller's UpsertArticle calls fight for the write
	// lock and hit SQLITE_BUSY even with a 5s busy_timeout (BUSY_SNAPSHOT in
	// WAL doesn't honor busy_timeout). One conn lets Go's database/sql queue
	// requests cleanly; reads block briefly when the poller writes but the
	// numbers are tiny for our workload (single-digit RPS).
	dbh.SetMaxOpenConns(1)
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
		slog.Default().Warn("db: PRAGMA optimize failed", "err", err)
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
