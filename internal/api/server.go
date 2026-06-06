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
	"github.com/brandonhon/ember/internal/summarize"
	"github.com/brandonhon/ember/internal/ttrss"
)

// PollerRefresher is the subset of *poller.Poller the API uses (lets us avoid
// importing poller here to keep dependencies one-directional).
type PollerRefresher interface {
	RefreshFeed(ctx context.Context, feedID int64) error
	EnqueueSummary(articleID int64) bool
	// ExtractArticle re-runs the readability extractor against the article's
	// URL and overwrites content_text + content_html when extraction yields
	// more text. Backs the "Re-extract" button in the reader pane.
	ExtractArticle(ctx context.Context, articleID int64) error
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
	TTRSS   *ttrss.Service // Tiny Tiny RSS starred/archived import; nil disables the endpoint
	StaticH http.Handler   // SPA / embed.FS handler; may be nil in tests
	// Ollama exposes the live summarizer so the admin LLM endpoints can list
	// installed models, pull new ones, and swap the active model. Optional —
	// nil when the summarizer is disabled or the noop (tests) is in use.
	Ollama *summarize.Ollama
	// WebAuthn drives passkey registration + assertion. Nil when EMBER_PUBLIC_URL
	// is not configured; the passkey endpoints then return 503.
	WebAuthn *auth.WebAuthn
	// TestMode loosens cookie Secure flag for non-HTTPS tests.
	TestMode bool
	// AllowPrivateURLs disables the SSRF block on outbound HTTP fetches for
	// homelab users who subscribe to LAN feeds. Off by default.
	AllowPrivateURLs bool
	// HSTSPreload appends "; preload" to the HSTS header. Enable only after
	// the domain is submitted to the browser preload list.
	HSTSPreload bool
	// TrustedProxies is the set of proxy CIDRs (strings) whose X-Real-IP /
	// X-Forwarded-Proto headers are honored. Empty = the app is the edge and
	// trusts only the connection peer.
	TrustedProxies []string
	// FreshWindow is the cutoff for the Fresh smart view — articles
	// published within this window count as "fresh". Surfaced to the
	// frontend via /api/me so isFresh() agrees with the server's
	// CountSmartViews query. Zero falls back to 6h.
	FreshWindow time.Duration
	// BackgroundCtx is the parent context for goroutines that a handler
	// detaches from the request lifecycle (e.g. initial feed refresh after
	// starter-pack import). Cancelled at process shutdown; nil falls back
	// to context.Background so tests that don't wire shutdown still work.
	BackgroundCtx context.Context
	// SMTPFallback is the env-derived SMTP config. The admin settings endpoints
	// resolve the live config by overlaying app_settings rows on top of this
	// fallback. Set from cfg at boot; never mutated after.
	SMTPFallback store.SMTPSettings
	// InitialBacklogHoursFallback is the env-derived default for the
	// first-ingest backlog window. The poller resolves the live value by
	// preferring an app_settings row over this fallback.
	InitialBacklogHoursFallback int
}

// backgroundCtx returns d.BackgroundCtx or context.Background if unset.
// Used by handlers that spawn detached goroutines.
func (d *Dependencies) backgroundCtx() context.Context {
	if d.BackgroundCtx != nil {
		return d.BackgroundCtx
	}
	return context.Background()
}

// NewRouter constructs the chi router. Public routes: /api/auth/*, /fever.
// All other /api/* require RequireAuth; /api/users/* admin actions require
// RequireAdmin. Non-/api routes fall back to the SPA.
func NewRouter(d Dependencies) http.Handler {
	trusted := ParseTrustedProxies(d.TrustedProxies)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(SecurityHeaders(trusted, d.HSTSPreload))
	r.Use(CSRFIssue(!d.TestMode))

	// 405 responses must carry the security headers too. chi's default
	// MethodNotAllowed handler runs outside the middleware chain, so register
	// one that re-applies SecurityHeaders before writing the JSON error.
	methodNotAllowed := SecurityHeaders(trusted, d.HSTSPreload)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}))
	r.MethodNotAllowed(methodNotAllowed.ServeHTTP)

	// Health endpoints — fast, no auth, no DB hit on /healthz; /readyz pings
	// DB. /healthz stays public because Caddy uses it for liveness probes.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Get("/readyz", d.handleReadyz)
	// /metrics is admin-only — exposing build version + error counters to
	// unauthenticated callers leaks operational state. Caddy can scrape it
	// over the internal docker network using an admin cookie.
	r.With(d.Auth.RequireAdmin).Get("/metrics", d.handleMetrics)

	// Per-IP rate limiter for the login endpoint to slow credential stuffing.
	// Generously sized in test mode so a full e2e run doesn't trip it.
	loginBurst := 10
	if d.TestMode {
		loginBurst = 1000
	}
	loginLimiter := NewRateLimiter(loginBurst, time.Minute, trusted)

	// Separate, higher-burst limiter for expensive authenticated endpoints
	// (outbound-fetch / goroutine-spawning / FTS work). Without a fronting
	// proxy to absorb floods, these are the cheapest way for a logged-in
	// client to pin CPU / open many outbound connections, so cap them.
	expensiveBurst := 30
	if d.TestMode {
		expensiveBurst = 1000
	}
	expensiveLimiter := NewRateLimiter(expensiveBurst, time.Minute, trusted)

	r.Route("/api", func(r chi.Router) {
		r.Use(CSRFVerify)
		// Auth — login is the only /api path that bypasses CSRFVerify (no
		// cookie yet on first call). The wrapping middleware checks for the
		// login suffix.
		r.With(loginLimiter.LimitMiddleware).Post("/auth/login", d.handleLogin)
		// Passkey login (public; rate-limited the same as password login).
		r.With(loginLimiter.LimitMiddleware).Post("/auth/passkey/begin", d.handlePasskeyLoginBegin)
		r.With(loginLimiter.LimitMiddleware).Post("/auth/passkey/finish", d.handlePasskeyLoginFinish)
		// Public probe that drives the login UI's passkey-button visibility.
		// Returns {any_registered: bool}. No auth, no CSRF (it's a GET).
		r.Get("/auth/passkey/exists", d.handlePasskeyExists)

		// Branding is auth-required so anonymous callers can't probe whether
		// an instance exists or what it's branded as. The login page renders
		// with the stock "Ember" name until a user signs in.
		r.With(d.Auth.RequireAuth).Get("/branding", d.handleGetBranding)
		r.With(d.Auth.RequireAdmin).Post("/admin/branding", d.handleSetBranding)
		r.With(d.Auth.RequireAuth).Post("/auth/logout", d.handleLogout)
		r.With(d.Auth.RequireAuth).Get("/me", d.handleMe)
		r.With(d.Auth.RequireAuth).Patch("/me/settings", d.handleUpdateSettings)
		r.With(d.Auth.RequireAuth).Post("/me/password", d.handleChangePassword)
		// Passkeys (self-service registration + management).
		r.With(d.Auth.RequireAuth).Get("/me/passkeys", d.handleListPasskeys)
		r.With(d.Auth.RequireAuth).Post("/me/passkeys/register/begin", d.handlePasskeyRegisterBegin)
		r.With(d.Auth.RequireAuth).Post("/me/passkeys/register/finish", d.handlePasskeyRegisterFinish)
		r.With(d.Auth.RequireAuth).Patch("/me/passkeys/{id}", d.handlePasskeyRename)
		r.With(d.Auth.RequireAuth).Delete("/me/passkeys/{id}", d.handlePasskeyDelete)

		// Users — list/get auth'd; mutation admin-only
		r.With(d.Auth.RequireAuth).Get("/users", d.handleListUsers)
		r.With(d.Auth.RequireAdmin).Post("/users", d.handleCreateUser)
		r.With(d.Auth.RequireAdmin).Patch("/users/{id}", d.handleUpdateUser)
		r.With(d.Auth.RequireAdmin).Delete("/users/{id}", d.handleDeleteUser)

		// Categories
		r.With(d.Auth.RequireAuth).Get("/categories", d.handleListCategories)
		r.With(d.Auth.RequireAuth).Post("/categories", d.handleCreateCategory)
		r.With(d.Auth.RequireAuth).Post("/categories/reorder", d.handleReorderCategories)
		r.With(d.Auth.RequireAuth).Patch("/categories/{id}", d.handleUpdateCategory)
		r.With(d.Auth.RequireAuth).Delete("/categories/{id}", d.handleDeleteCategory)

		// Feeds / subscriptions
		r.With(d.Auth.RequireAuth).Get("/feeds", d.handleListFeeds)
		r.With(d.Auth.RequireAuth, expensiveLimiter.LimitMiddleware).Post("/feeds", d.handleAddFeed)
		r.With(d.Auth.RequireAuth, expensiveLimiter.LimitMiddleware).Post("/feeds/discover", d.handleDiscoverFeeds)
		r.With(d.Auth.RequireAuth).Post("/feeds/reorder", d.handleReorderFeeds)
		r.With(d.Auth.RequireAuth).Patch("/feeds/{id}", d.handleUpdateFeed)
		r.With(d.Auth.RequireAuth).Delete("/feeds/{id}", d.handleDeleteFeed)
		r.With(d.Auth.RequireAuth, expensiveLimiter.LimitMiddleware).Post("/feeds/{id}/refresh", d.handleRefreshFeed)
		r.With(d.Auth.RequireAuth, expensiveLimiter.LimitMiddleware).Post("/feeds/{id}/resummarize", d.handleResummarizeFeed)
		r.With(d.Auth.RequireAdmin, expensiveLimiter.LimitMiddleware).Post("/feeds/resummarize-all", d.handleResummarizeAll)

		// LLM admin
		r.With(d.Auth.RequireAdmin).Get("/admin/llm", d.handleGetLLM)
		r.With(d.Auth.RequireAdmin).Post("/admin/llm/model", d.handleSetLLMModel)
		r.With(d.Auth.RequireAdmin).Post("/admin/llm/pull", d.handlePullLLMModel)
		r.With(d.Auth.RequireAdmin).Post("/admin/llm/delete", d.handleDeleteLLMModel)
		r.With(d.Auth.RequireAdmin).Post("/admin/llm/options", d.handleSetLLMOptions)

		// DB admin
		r.With(d.Auth.RequireAdmin).Get("/admin/db", d.handleGetDB)
		r.With(d.Auth.RequireAdmin).Post("/admin/db/backup", d.handleDBBackup)
		r.With(d.Auth.RequireAdmin).Post("/admin/db/cleanup", d.handleDBCleanup)
		r.With(d.Auth.RequireAdmin).Post("/admin/db/schedule", d.handleDBSchedule)
		r.With(d.Auth.RequireAuth, expensiveLimiter.LimitMiddleware).Post("/feeds/import", d.handleOPMLImport)
		r.With(d.Auth.RequireAuth, expensiveLimiter.LimitMiddleware).Post("/feeds/import-ttrss", d.handleTTRSSImport)
		r.With(d.Auth.RequireAuth, expensiveLimiter.LimitMiddleware).Post("/feeds/import-ttrss-api", d.handleTTRSSAPIImport)
		r.With(d.Auth.RequireAuth).Get("/feeds/export", d.handleOPMLExport)

		// Starter packs
		r.With(d.Auth.RequireAuth).Get("/starter-packs", d.handleListStarterPacks)
		r.With(d.Auth.RequireAuth, expensiveLimiter.LimitMiddleware).Post("/starter-packs/{slug}", d.handleImportStarterPack)
		r.With(d.Auth.RequireAuth).Delete("/starter-packs/{slug}", d.handleRemoveStarterPack)

		// Articles
		r.With(d.Auth.RequireAuth).Get("/articles", d.handleListArticles)
		r.With(d.Auth.RequireAuth).Get("/articles/{id}", d.handleGetArticle)
		r.With(d.Auth.RequireAuth).Post("/articles/read", d.handleSetRead)
		r.With(d.Auth.RequireAuth).Post("/articles/star", d.handleSetStar)
		r.With(d.Auth.RequireAuth).Post("/articles/later", d.handleSetLater)
		r.With(d.Auth.RequireAuth).Post("/articles/mark-all-read", d.handleMarkAllRead)
		r.With(d.Auth.RequireAuth, expensiveLimiter.LimitMiddleware).Post("/articles/{id}/extract", d.handleReExtractArticle)

		// Per-article user tags
		r.With(d.Auth.RequireAuth).Get("/articles/{id}/tags", d.handleListArticleTags)
		r.With(d.Auth.RequireAuth).Post("/articles/{id}/tags", d.handleAddArticleTag)
		r.With(d.Auth.RequireAuth).Delete("/articles/{id}/tags", d.handleRemoveArticleTag)
		r.With(d.Auth.RequireAuth).Get("/tags", d.handleListUserTags)
		r.With(d.Auth.RequireAuth).Get("/me/stats", d.handleGetStats)
		r.With(d.Auth.RequireAuth).Get("/me/smart-counts", d.handleSmartCounts)
		r.With(d.Auth.RequireAuth).Get("/me/digest", d.handleGetDigest)
		r.With(d.Auth.RequireAuth).Post("/me/digest", d.handleSetDigest)

		// Admin session policy (server-wide TTL). Per-user TTL is not
		// supported — see internal/api/session_handlers.go for rationale.
		r.With(d.Auth.RequireAdmin).Get("/admin/session", d.handleGetSessionTTL)
		r.With(d.Auth.RequireAdmin).Post("/admin/session/ttl", d.handleSetSessionTTL)

		// Admin settings: SMTP + first-ingest backlog window. SMTP password
		// is write-only — GET returns whether one is set, not the value.
		r.With(d.Auth.RequireAdmin).Get("/admin/settings", d.handleGetAdminSettings)
		r.With(d.Auth.RequireAdmin).Patch("/admin/settings", d.handleSetAdminSettings)
		r.With(d.Auth.RequireAdmin).Post("/admin/settings/email-test", d.handleTestEmail)

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
		r.With(d.Auth.RequireAuth, expensiveLimiter.LimitMiddleware).Get("/search", d.handleSearch)
		r.With(d.Auth.RequireAuth).Get("/saved-searches", d.handleListSavedSearches)
		r.With(d.Auth.RequireAuth).Post("/saved-searches", d.handleCreateSavedSearch)
		r.With(d.Auth.RequireAuth).Delete("/saved-searches/{id}", d.handleDeleteSavedSearch)

		// Catch-all under /api/ → 404 JSON
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusNotFound, "not_found", "unknown API endpoint")
		})
	})

	// Fever shim (public path, auth via per-user random token in form body).
	// The token is 256-bit random so brute force is infeasible; the limiter
	// is here to bound the cost of unauthenticated requests, each of which
	// would otherwise force a full ListUsers scan.
	r.With(loginLimiter.LimitMiddleware).Post("/fever", d.handleFever)
	r.With(loginLimiter.LimitMiddleware).Get("/fever", d.handleFever)

	// Static SPA / embed fallback
	if d.StaticH != nil {
		r.Handle("/*", d.StaticH)
	}

	return r
}
