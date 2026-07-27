package api

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/opml"
	"github.com/brandonhon/ember/internal/push"
	"github.com/brandonhon/ember/internal/store"
	"github.com/brandonhon/ember/internal/summarize"
	"github.com/brandonhon/ember/internal/ttrss"
	"github.com/brandonhon/ember/internal/updatecheck"
)

// pollerRefresher is the subset of *poller.Poller the API uses (lets us avoid
// importing poller here to keep dependencies one-directional).
type pollerRefresher interface {
	RefreshFeed(ctx context.Context, feedID int64) error
	EnqueueSummary(articleID int64) bool
	// ExtractArticle re-runs the readability extractor against the article's
	// URL and overwrites content_text + content_html when extraction yields
	// more text. Backs the "Re-extract" button in the reader pane.
	ExtractArticle(ctx context.Context, articleID int64) error
}

// metricsSnapshotter is implemented by the poller; lets /metrics export
// counters without depending on the poller package directly.
type metricsSnapshotter interface {
	MetricsSnapshot() map[string]int64
}

// updateStatus is the subset of *updatecheck.Checker the API reads — the cached
// latest-release result. ok is false until the first check completes or when
// the check is disabled. Injecting it as an interface lets tests stub it.
type updateStatus interface {
	Latest() (updatecheck.Result, bool)
}

// Dependencies wires the API.
type Dependencies struct {
	Store   *store.Store
	Auth    *auth.Auth
	Poller  pollerRefresher
	Metrics metricsSnapshotter
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
	// PollMinIntervalFallback is the env-derived default for the adaptive
	// fetch-interval floor; the settings endpoints overlay an app_settings
	// row on it.
	PollMinIntervalFallback time.Duration
	// Push fans out Web Push notifications. Nil disables the feature
	// (the /api/me/push-* endpoints return 503).
	Push *push.Notifier
	// EmailDomain is the operator-configured EMBER_EMAIL_DOMAIN. When
	// empty, the email-inbox endpoints return enabled=false / 503 and
	// the SMTP listener doesn't start.
	EmailDomain string
	// SessionKey is the EMBER_SESSION_KEY. Used to derive the image-proxy
	// signing key so /api/img only fetches URLs ember itself signed.
	SessionKey string
	// UpdateChecker holds the cached latest-release result from GitHub. Nil
	// disables the update hint; /api/me then omits the "update" object. Only
	// admins see the result (see handleMe).
	UpdateChecker updateStatus
	// UpdateCheckEnabledFallback is the env-derived default (the negation of
	// EMBER_DISABLE_UPDATE_CHECK) for the update_check_enabled admin setting.
	UpdateCheckEnabledFallback bool
	// PasskeyRequireUVFallback is the env-derived default
	// (EMBER_PASSKEY_REQUIRE_UV) for the passkey_require_uv admin setting.
	PasskeyRequireUVFallback bool

	// img signs + serves the same-origin image proxy. Built in NewRouter
	// from SessionKey; never set by callers.
	img *imageProxy
	// trustedNets is TrustedProxies parsed once in NewRouter, so handlers can
	// attribute a request to a real client IP for security logging without
	// re-parsing CIDRs per request. Never set by callers.
	trustedNets []*net.IPNet
}

// summariesOn reports whether AI summarization is wired up (an Ollama backend
// is configured). It is the single switch for the article summary gate: when
// on, every reading view + every count hides articles the summarizer hasn't
// stamped yet; when off, the gate is bypassed everywhere. Mirrors the
// summaries_enabled flag surfaced to the SPA via /api/me.
func (d *Dependencies) summariesOn() bool { return d.Ollama != nil }

// defaultFreshWindow is the Fresh-view cutoff used when the operator hasn't
// configured one. Mirrored by the SPA's isFresh() via /api/me.
const defaultFreshWindow = 6 * time.Hour

// freshWindow is the configured Fresh-view cutoff, falling back to
// defaultFreshWindow.
func (d *Dependencies) freshWindow() time.Duration {
	if d.FreshWindow > 0 {
		return d.FreshWindow
	}
	return defaultFreshWindow
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
	trusted := parseTrustedProxies(d.TrustedProxies)
	d.trustedNets = trusted

	// Same-origin image proxy. Article responses rewrite image_url to a signed
	// /api/img path so content blockers don't strip publisher-CDN lead images.
	d.img = newImageProxy(d.SessionKey, d.AllowPrivateURLs)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(securityHeaders(trusted, d.HSTSPreload))
	r.Use(csrfIssue(!d.TestMode))

	// 405 responses must carry the security headers too. chi's default
	// MethodNotAllowed handler runs outside the middleware chain, so register
	// one that re-applies securityHeaders before writing the JSON error.
	methodNotAllowed := securityHeaders(trusted, d.HSTSPreload)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	loginLimiter := newRateLimiter(loginBurst, time.Minute, trusted)

	// Separate, higher-burst limiter for expensive authenticated endpoints
	// (outbound-fetch / goroutine-spawning / FTS work). Without a fronting
	// proxy to absorb floods, these are the cheapest way for a logged-in
	// client to pin CPU / open many outbound connections, so cap them.
	expensiveBurst := 30
	if d.TestMode {
		expensiveBurst = 1000
	}
	expensiveLimiter := newRateLimiter(expensiveBurst, time.Minute, trusted)

	r.Route("/api", func(r chi.Router) {
		r.Use(csrfVerify)

		// --- Public (pre-session) -------------------------------------------
		// Login is the only /api path that bypasses csrfVerify (no cookie yet
		// on the first call); the CSRF middleware checks for the login suffix.
		r.With(loginLimiter.limitMiddleware).Post("/auth/login", d.handleLogin)
		// Passkey login — rate-limited the same as password login.
		r.With(loginLimiter.limitMiddleware).Post("/auth/passkey/begin", d.handlePasskeyLoginBegin)
		r.With(loginLimiter.limitMiddleware).Post("/auth/passkey/finish", d.handlePasskeyLoginFinish)
		// Probe driving the login UI's passkey-button visibility. Returns
		// {any_registered: bool}. No auth, no CSRF (it's a GET).
		r.Get("/auth/passkey/exists", d.handlePasskeyExists)

		// --- Signed-in users ------------------------------------------------
		// Everything in this group requires a session. Routes that spawn
		// goroutines, run FTS, or make outbound fetches additionally carry the
		// expensive limiter — without a fronting proxy they are the cheapest
		// way for a logged-in client to pin CPU or open many connections.
		r.Group(func(r chi.Router) {
			r.Use(d.Auth.RequireAuth)

			// Branding is auth-required so anonymous callers can't probe
			// whether an instance exists or what it's branded as. The login
			// page renders with the stock "Ember" name until a user signs in.
			r.Get("/branding", d.handleGetBranding)
			r.Post("/auth/logout", d.handleLogout)
			r.Get("/me", d.handleMe)
			r.Patch("/me/settings", d.handleUpdateSettings)
			r.Patch("/me/email", d.handleUpdateEmail)
			r.Post("/me/password", d.handleChangePassword)

			// Passkeys (self-service registration + management).
			r.Get("/me/passkeys", d.handleListPasskeys)
			r.Post("/me/passkeys/register/begin", d.handlePasskeyRegisterBegin)
			r.Post("/me/passkeys/register/finish", d.handlePasskeyRegisterFinish)
			r.Patch("/me/passkeys/{id}", d.handlePasskeyRename)
			r.Delete("/me/passkeys/{id}", d.handlePasskeyDelete)

			// Users — list is readable by any signed-in user (it backs the
			// share-modal picker, and returns a minimal projection to
			// non-admins); every mutation is admin-only, below.
			r.Get("/users", d.handleListUsers)

			// Categories
			r.Get("/categories", d.handleListCategories)
			r.Post("/categories", d.handleCreateCategory)
			r.Post("/categories/reorder", d.handleReorderCategories)
			r.Patch("/categories/{id}", d.handleUpdateCategory)
			r.Delete("/categories/{id}", d.handleDeleteCategory)

			// Feeds / subscriptions
			r.Get("/feeds", d.handleListFeeds)
			r.With(expensiveLimiter.limitMiddleware).Post("/feeds", d.handleAddFeed)
			r.With(expensiveLimiter.limitMiddleware).Post("/feeds/discover", d.handleDiscoverFeeds)
			r.Post("/feeds/reorder", d.handleReorderFeeds)
			r.With(expensiveLimiter.limitMiddleware).Patch("/feeds/{id}", d.handleUpdateFeed)
			r.Delete("/feeds/{id}", d.handleDeleteFeed)
			r.With(expensiveLimiter.limitMiddleware).Post("/feeds/refresh", d.handleRefreshAllFeeds)
			r.With(expensiveLimiter.limitMiddleware).Post("/feeds/{id}/refresh", d.handleRefreshFeed)
			r.With(expensiveLimiter.limitMiddleware).Post("/feeds/{id}/resummarize", d.handleResummarizeFeed)

			// Import / export
			r.With(expensiveLimiter.limitMiddleware).Post("/feeds/import", d.handleOPMLImport)
			r.With(expensiveLimiter.limitMiddleware).Post("/feeds/import-ttrss", d.handleTTRSSImport)
			r.With(expensiveLimiter.limitMiddleware).Post("/feeds/import-ttrss-api", d.handleTTRSSAPIImport)
			r.Get("/feeds/export", d.handleOPMLExport)

			// Starter packs
			r.Get("/starter-packs", d.handleListStarterPacks)
			r.With(expensiveLimiter.limitMiddleware).Post("/starter-packs/{slug}", d.handleImportStarterPack)
			r.Delete("/starter-packs/{slug}", d.handleRemoveStarterPack)

			// Same-origin image proxy for article lead images. Signed URLs
			// only (a capability), so it's not an open relay; limited like the
			// other outbound-fetch endpoints since each request opens an
			// origin connection.
			r.With(expensiveLimiter.limitMiddleware).Get("/img", d.img.handle)

			// Articles
			r.Get("/articles", d.handleListArticles)
			r.Get("/articles/{id}", d.handleGetArticle)
			r.Get("/articles/{id}/cluster", d.handleGetArticleCluster)
			r.Post("/articles/read", d.handleSetRead)
			r.Post("/articles/star", d.handleSetStar)
			r.Post("/articles/later", d.handleSetLater)
			r.Post("/articles/mark-all-read", d.handleMarkAllRead)
			r.With(expensiveLimiter.limitMiddleware).Post("/articles/{id}/extract", d.handleReExtractArticle)

			// Per-article user tags
			r.Get("/articles/{id}/tags", d.handleListArticleTags)
			r.Post("/articles/{id}/tags", d.handleAddArticleTag)
			r.Delete("/articles/{id}/tags", d.handleRemoveArticleTag)
			r.Get("/tags", d.handleListUserTags)

			// Web Push (VAPID) — public key fetch, subscription CRUD, test
			// send. All 503 if d.Push is nil. See internal/push.
			r.Get("/me/push-vapid-public-key", d.handleGetVapidKey)
			r.Get("/me/push-subscriptions", d.handleListPushSubscriptions)
			r.Post("/me/push-subscriptions", d.handleCreatePushSubscription)
			r.Delete("/me/push-subscriptions/{id}", d.handleDeletePushSubscription)
			r.With(expensiveLimiter.limitMiddleware).Post("/me/push-subscriptions/test", d.handleTestPushNotification)

			// Email newsletter inbox (per-user address). Always registered;
			// the handlers report enabled=false / 503 when EMBER_EMAIL_DOMAIN
			// isn't configured.
			r.Get("/me/inbox", d.handleGetInbox)
			r.With(expensiveLimiter.limitMiddleware).Post("/me/inbox/rotate", d.handleRotateInbox)

			r.Get("/me/stats", d.handleGetStats)
			r.Get("/me/smart-counts", d.handleSmartCounts)
			r.Get("/me/digest", d.handleGetDigest)
			r.Post("/me/digest", d.handleSetDigest)

			// Boards
			r.Get("/boards", d.handleListBoards)
			r.Post("/boards", d.handleCreateBoard)
			r.Delete("/boards/{id}", d.handleDeleteBoard)
			r.Post("/boards/{id}/articles", d.handleBoardAdd)
			r.Delete("/boards/{id}/articles/{articleId}", d.handleBoardRemove)

			// Shares
			r.Post("/shares", d.handleCreateShare)
			r.Get("/shares/inbox", d.handleListInbox)
			r.Post("/shares/{id}/seen", d.handleMarkShareSeen)

			// Filters
			r.Get("/filters", d.handleListFilters)
			r.Post("/filters", d.handleCreateFilter)
			r.Get("/filters/export", d.handleExportFilters)
			r.Post("/filters/import", d.handleImportFilters)
			r.Post("/filters/preview", d.handlePreviewFilter)
			r.Patch("/filters/{id}", d.handleUpdateFilter)
			r.Delete("/filters/{id}", d.handleDeleteFilter)

			// Search
			r.With(expensiveLimiter.limitMiddleware).Get("/search", d.handleSearch)
			r.Get("/saved-searches", d.handleListSavedSearches)
			r.Post("/saved-searches", d.handleCreateSavedSearch)
			r.Delete("/saved-searches/{id}", d.handleDeleteSavedSearch)
		})

		// --- Admins ---------------------------------------------------------
		// Server-wide configuration, destructive maintenance, and work spent on
		// every user's behalf. RequireAdmin 401s the anonymous caller and 403s
		// the signed-in reader.
		r.Group(func(r chi.Router) {
			r.Use(d.Auth.RequireAdmin)

			r.Post("/admin/branding", d.handleSetBranding)
			r.Patch("/users/{id}", d.handleUpdateUser)
			r.Post("/users", d.handleCreateUser)
			r.Delete("/users/{id}", d.handleDeleteUser)

			// Resummarize-all rewrites every article in the database, so it is
			// both admin-only and rate-limited.
			r.With(expensiveLimiter.limitMiddleware).Post("/feeds/resummarize-all", d.handleResummarizeAll)

			// LLM
			r.Get("/admin/llm", d.handleGetLLM)
			r.Post("/admin/llm/model", d.handleSetLLMModel)
			r.Post("/admin/llm/pull", d.handlePullLLMModel)
			r.Post("/admin/llm/delete", d.handleDeleteLLMModel)
			r.Post("/admin/llm/options", d.handleSetLLMOptions)

			// DB maintenance
			r.Get("/admin/db", d.handleGetDB)
			r.Post("/admin/db/backup", d.handleDBBackup)
			r.Delete("/admin/db/backups/{name}", d.handleDeleteBackup)
			r.Post("/admin/db/opml-export", d.handleOPMLExportNow)
			r.Delete("/admin/db/exports/{name}", d.handleDeleteExport)
			r.Post("/admin/db/cleanup", d.handleDBCleanup)
			r.Post("/admin/db/schedule", d.handleDBSchedule)

			// Session policy (server-wide TTL). Per-user TTL is not supported —
			// see internal/api/session_handlers.go for the rationale.
			r.Get("/admin/session", d.handleGetSessionTTL)
			r.Post("/admin/session/ttl", d.handleSetSessionTTL)

			// Settings: SMTP + windows + backlog. The SMTP password is
			// write-only — GET reports whether one is set, not the value.
			r.Get("/admin/settings", d.handleGetAdminSettings)
			r.Patch("/admin/settings", d.handleSetAdminSettings)
			r.Post("/admin/settings/email-test", d.handleTestEmail)
		})

		// Catch-all under /api/ → 404 JSON
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusNotFound, "not_found", "unknown API endpoint")
		})
	})

	// Fever shim (public path, auth via per-user random token in form body).
	// The token is 256-bit random so brute force is infeasible; the limiter
	// is here to bound the cost of unauthenticated requests, each of which
	// would otherwise force a full ListUsers scan.
	r.With(loginLimiter.limitMiddleware).Post("/fever", d.handleFever)
	r.With(loginLimiter.limitMiddleware).Get("/fever", d.handleFever)

	// Static SPA / embed fallback
	if d.StaticH != nil {
		r.Handle("/*", d.StaticH)
	}

	return r
}
