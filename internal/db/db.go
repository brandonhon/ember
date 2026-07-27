// Package db opens the SQLite database, applies PRAGMAs, and runs migrations.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	_ "modernc.org/sqlite" // sqlite driver (pure Go, CGO-free)
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// tuning is the single source of truth for the connection PRAGMAs. They are
// applied via the DSN so EVERY pooled connection gets them — an earlier
// version also kept a duplicate PRAGMA script that only ran on one connection,
// which meant the rest of the pool silently ran untuned. Keep this list as the
// only place they are declared.
//
//   - journal_mode=WAL: concurrent readers + one writer
//   - foreign_keys=ON: enforce referential integrity
//   - busy_timeout=5000: wait instead of SQLITE_BUSY on contention
//   - synchronous=NORMAL: safe under WAL, ~2x faster than FULL for our writes
//   - temp_store=MEMORY: temp tables in RAM
//   - cache_size=-65536: 64 MiB page cache (default 2 MiB is far too small for
//     the nested article queries with dedup joins)
//   - mmap_size=268435456: 256 MiB memory-mapped IO for read-heavy paths
var tuning = []struct{ name, value string }{
	{"busy_timeout", "5000"},
	{"foreign_keys", "ON"},
	{"journal_mode", "WAL"},
	{"synchronous", "NORMAL"},
	{"temp_store", "MEMORY"},
	{"cache_size", "-65536"},
	{"mmap_size", "268435456"},
}

// dsn builds the driver DSN for path with every tuning pragma attached.
// Entries in override replace the shared value for that pragma, and any extra
// keys are appended. Overriding in place matters: the driver applies the FIRST
// occurrence of a pragma, so simply appending a second cache_size does nothing
// (verified — TestReadPool_UsesSmallerPageCache caught exactly that).
func dsn(path string, override ...struct{ name, value string }) string {
	vals := make(map[string]string, len(override))
	order := make([]string, 0, len(tuning)+len(override))
	for _, p := range tuning {
		vals[p.name] = p.value
		order = append(order, p.name)
	}
	for _, o := range override {
		if _, exists := vals[o.name]; !exists {
			order = append(order, o.name)
		}
		vals[o.name] = o.value
	}
	var b strings.Builder
	b.WriteString(path)
	for i, name := range order {
		if i == 0 {
			b.WriteString("?")
		} else {
			b.WriteString("&")
		}
		b.WriteString("_pragma=" + name + "(" + vals[name] + ")")
	}
	return b.String()
}

// Open opens the SQLite database at path, applies PRAGMAs, and runs all
// pending migrations. Returns the database handle.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	dbh, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	// Serialize through a single connection. SQLite has one writer; with
	// multiple Go conns the poller's UpsertArticle calls fight for the write
	// lock and hit SQLITE_BUSY even with a 5s busy_timeout (BUSY_SNAPSHOT in
	// WAL doesn't honor busy_timeout). One conn lets Go's database/sql queue
	// requests cleanly; reads block briefly when the poller writes but the
	// numbers are tiny for our workload (single-digit RPS).
	//
	// NOTE: because every request shares this one connection, a slow query
	// blocks the whole app for its duration — which is why the article-count
	// predicates in internal/store are written to stay index-driven.
	dbh.SetMaxOpenConns(1)
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

// readPoolConns is how many concurrent read connections OpenRead allows.
// SQLite in WAL mode serves many readers alongside the single writer, so the
// only real cost is file descriptors. Four covers the SPA's parallel
// sidebar/list/count fetches for several users without being extravagant on a
// self-hosted box.
const readPoolConns = 4

// readPoolCacheSize is the per-read-connection page cache, in negative KiB
// (SQLite's convention). 16 MiB x readPoolConns keeps the pool's total near
// the single write connection's 64 MiB rather than multiplying it.
const readPoolCacheSize = "-16384"

// OpenRead opens a SECOND handle to an already-migrated database, for
// read-only queries.
//
// Why this exists: Open caps the pool at one connection because SQLite has a
// single writer, and multiple writing connections hit SQLITE_BUSY that
// busy_timeout does not cover (BUSY_SNAPSHOT). That cap also serialises every
// READ behind whatever the poller happens to be writing — measured at 166ms
// worst-case reads under ingest, versus 23ms across a separate pool.
//
// Memory: the shared tuning sets cache_size to 64 MiB PER CONNECTION, so four
// read connections at that size would take the page-cache ceiling from 64 MiB
// to 320 MiB — unacceptable on the small self-hosted boxes ember targets. The
// read pool therefore overrides cache_size to 16 MiB, keeping the total budget
// roughly where it was (1x64 write + 4x16 read). Readers share the OS page
// cache and the 256 MiB mmap window anyway, so the smaller per-connection
// cache costs very little; measured throughput was unchanged.
//
// The connections are opened with query_only, so SQLite itself rejects any
// write attempted here. A store method routed to the read pool by mistake
// fails immediately and loudly instead of silently reintroducing the write
// contention this design exists to avoid.
//
// Callers must run Open (which migrates) first; OpenRead deliberately does not
// migrate, so it can never race the writer's schema work.
func OpenRead(ctx context.Context, path string) (*sql.DB, error) {
	dbh, err := sql.Open("sqlite", dsn(path,
		struct{ name, value string }{"query_only", "true"},
		struct{ name, value string }{"cache_size", readPoolCacheSize},
	))
	if err != nil {
		return nil, fmt.Errorf("open sqlite (read) %q: %w", path, err)
	}
	dbh.SetMaxOpenConns(readPoolConns)
	if err := dbh.PingContext(ctx); err != nil {
		dbh.Close()
		return nil, fmt.Errorf("ping sqlite (read) %q: %w", path, err)
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
