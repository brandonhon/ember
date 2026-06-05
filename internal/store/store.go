// Package store provides the data-access layer over the ember SQLite database.
// All methods are user-scoped where applicable so the API layer cannot
// accidentally leak data between users.
package store

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/db"
)

// ErrNotFound is returned when a uniquely-identified row does not exist.
var ErrNotFound = errors.New("store: not found")

// ErrForbidden is returned when a row exists but is owned by a different user.
var ErrForbidden = errors.New("store: forbidden")

// ErrConflict is returned for unique-constraint violations (duplicate username,
// duplicate category name within a user, etc.).
var ErrConflict = errors.New("store: conflict")

// ErrInvalidQuery is returned by Search when the user's text is not a valid
// FTS5 MATCH expression (unbalanced quote, bare boolean operator, bad column
// filter). The api layer maps it to 400 rather than a 500.
var ErrInvalidQuery = errors.New("store: invalid search query")

// ErrNoNewContent is returned by Poller.ExtractArticle when readability ran
// but the result wasn't an improvement over the stored body. Defined here so
// the api package can errors.Is without importing poller (which would create
// a cycle through PollerRefresher).
var ErrNoNewContent = errors.New("store: re-extract produced no new content")

// Store wraps a sql.DB with all the data-access methods ember needs. Construct
// once and share — sql.DB is concurrency-safe.
type Store struct {
	DB  *sql.DB
	Now func() time.Time // injectable clock for tests
}

// New returns a Store backed by the given handle. Uses time.Now by default.
func New(dbh *sql.DB) *Store {
	return &Store{DB: dbh, Now: time.Now}
}

// nowUnix returns the current store time in Unix seconds.
func (s *Store) nowUnix() int64 {
	return s.Now().Unix()
}

// NewTest returns a Store backed by an isolated temp SQLite database. The
// returned clock can be advanced by tests.
func NewTest(t *testing.T) *Store {
	t.Helper()
	dbh := db.OpenTest(t)
	return New(dbh)
}
