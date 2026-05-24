package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/opml"
	"github.com/brandonhon/ember/internal/store"
)

// PollerRefresher is the subset of *poller.Poller the API uses (lets us avoid
// importing poller here to keep dependencies one-directional).
type PollerRefresher interface {
	RefreshFeed(ctx context.Context, feedID int64) error
	EnqueueSummary(articleID int64) bool
}

// MetricsSnapshotter is implemented by the poller; lets /metrics export
// counters without depending on the poller package directly.
type MetricsSnapshotter interface {
	MetricsSnapshot() map[string]int64
}

// Dependencies wires the API.
type Dependencies struct {
	Store   *store.Store
	Auth    *auth.Auth
	Poller  PollerRefresher
	Metrics MetricsSnapshotter
	OPML    *opml.Service
	StaticH http.Handler // SPA / embed.FS handler; may be nil in tests
	// TestMode loosens cookie Secure flag for non-HTTPS tests.
	TestMode bool
}

// NewRouter constructs the chi router. Public routes: /api/auth/*, /fever.
// All other /api/* require RequireAuth; /api/users/* admin actions require
// RequireAdmin. Non-/api routes fall back to the SPA.
func NewRouter(d Dependencies) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestLogger())
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(SecurityHeaders)
	r.Use(CSRFIssue(!d.TestMode))

	// Health endpoints — fast, no auth, no DB hit on /healthz; /readyz pings DB.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Get("/readyz", d.handleReadyz)
	r.Get("/metrics", d.handleMetrics)

	// Per-IP rate limiter for the login endpoint to slow credential stuffing.
	// Generously sized in test mode so a full e2e run doesn't trip it.
	loginBurst := 10
	if d.TestMode {
		loginBurst = 1000
	}
	loginLimiter := NewRateLimiter(loginBurst, time.Minute)

	r.Route("/api", func(r chi.Router) {
		r.Use(CSRFVerify)
		// Auth — login is the only /api path that bypasses CSRFVerify (no
		// cookie yet on first call). The wrapping middleware checks for the
		// login suffix.
		r.With(loginLimiter.LimitMiddleware).Post("/auth/login", d.handleLogin)
		r.With(d.Auth.RequireAuth).Post("/auth/logout", d.handleLogout)
		r.With(d.Auth.RequireAuth).Get("/me", d.handleMe)
		r.With(d.Auth.RequireAuth).Patch("/me/settings", d.handleUpdateSettings)
		r.With(d.Auth.RequireAuth).Post("/me/password", d.handleChangePassword)

		// Users — list/get auth'd; mutation admin-only
		r.With(d.Auth.RequireAuth).Get("/users", d.handleListUsers)
		r.With(d.Auth.RequireAdmin).Post("/users", d.handleCreateUser)
		r.With(d.Auth.RequireAdmin).Patch("/users/{id}", d.handleUpdateUser)
		r.With(d.Auth.RequireAdmin).Delete("/users/{id}", d.handleDeleteUser)

		// Categories
		r.With(d.Auth.RequireAuth).Get("/categories", d.handleListCategories)
		r.With(d.Auth.RequireAuth).Post("/categories", d.handleCreateCategory)
		r.With(d.Auth.RequireAuth).Patch("/categories/{id}", d.handleUpdateCategory)
		r.With(d.Auth.RequireAuth).Delete("/categories/{id}", d.handleDeleteCategory)

		// Feeds / subscriptions
		r.With(d.Auth.RequireAuth).Get("/feeds", d.handleListFeeds)
		r.With(d.Auth.RequireAuth).Post("/feeds", d.handleAddFeed)
		r.With(d.Auth.RequireAuth).Patch("/feeds/{id}", d.handleUpdateFeed)
		r.With(d.Auth.RequireAuth).Delete("/feeds/{id}", d.handleDeleteFeed)
		r.With(d.Auth.RequireAuth).Post("/feeds/{id}/refresh", d.handleRefreshFeed)
		r.With(d.Auth.RequireAuth).Post("/feeds/{id}/resummarize", d.handleResummarizeFeed)
		r.With(d.Auth.RequireAuth).Post("/feeds/import", d.handleOPMLImport)
		r.With(d.Auth.RequireAuth).Get("/feeds/export", d.handleOPMLExport)

		// Articles
		r.With(d.Auth.RequireAuth).Get("/articles", d.handleListArticles)
		r.With(d.Auth.RequireAuth).Get("/articles/{id}", d.handleGetArticle)
		r.With(d.Auth.RequireAuth).Post("/articles/read", d.handleSetRead)
		r.With(d.Auth.RequireAuth).Post("/articles/star", d.handleSetStar)
		r.With(d.Auth.RequireAuth).Post("/articles/later", d.handleSetLater)
		r.With(d.Auth.RequireAuth).Post("/articles/mark-all-read", d.handleMarkAllRead)

		// Boards
		r.With(d.Auth.RequireAuth).Get("/boards", d.handleListBoards)
		r.With(d.Auth.RequireAuth).Post("/boards", d.handleCreateBoard)
		r.With(d.Auth.RequireAuth).Delete("/boards/{id}", d.handleDeleteBoard)
		r.With(d.Auth.RequireAuth).Post("/boards/{id}/articles", d.handleBoardAdd)
		r.With(d.Auth.RequireAuth).Delete("/boards/{id}/articles/{articleId}", d.handleBoardRemove)

		// Shares
		r.With(d.Auth.RequireAuth).Post("/shares", d.handleCreateShare)
		r.With(d.Auth.RequireAuth).Get("/shares/inbox", d.handleListInbox)
		r.With(d.Auth.RequireAuth).Post("/shares/{id}/seen", d.handleMarkShareSeen)

		// Filters
		r.With(d.Auth.RequireAuth).Get("/filters", d.handleListFilters)
		r.With(d.Auth.RequireAuth).Post("/filters", d.handleCreateFilter)
		r.With(d.Auth.RequireAuth).Patch("/filters/{id}", d.handleUpdateFilter)
		r.With(d.Auth.RequireAuth).Delete("/filters/{id}", d.handleDeleteFilter)

		// Search
		r.With(d.Auth.RequireAuth).Get("/search", d.handleSearch)

		// Catch-all under /api/ → 404 JSON
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusNotFound, "not_found", "unknown API endpoint")
		})
	})

	// Fever shim (public path, auth via md5 key in body)
	r.Post("/fever", d.handleFever)
	r.Get("/fever", d.handleFever)

	// Static SPA / embed fallback
	if d.StaticH != nil {
		r.Handle("/*", d.StaticH)
	}

	return r
}

// requestLogger is a slim slog-based middleware.
func requestLogger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}
}
