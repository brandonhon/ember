// ember is a self-hosted RSS/Atom reader. The binary embeds a Svelte SPA,
// runs the JSON API + Fever shim, and runs a background poller.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/brandonhon/ember/internal/api"
	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/config"
	"github.com/brandonhon/ember/internal/db"
	"github.com/brandonhon/ember/internal/digest"
	"github.com/brandonhon/ember/internal/feed"
	"github.com/brandonhon/ember/internal/opml"
	"github.com/brandonhon/ember/internal/poller"
	"github.com/brandonhon/ember/internal/store"
	"github.com/brandonhon/ember/internal/summarize"
	"github.com/brandonhon/ember/internal/sysinfo"
	"github.com/brandonhon/ember/internal/ttrss"
	"github.com/brandonhon/ember/internal/urlcheck"
	"github.com/brandonhon/ember/internal/web"
)

// version is set via -ldflags at build time.
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-v", "--version", "version":
			fmt.Printf("ember %s\n", version)
			return
		case "probe":
			runProbe()
			return
		}
	}
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "ember: %v\n", err)
		os.Exit(1)
	}
}

// runProbe prints detected host specs and a recommended Ollama model. Useful
// at install time: `docker run --rm ember probe`.
func runProbe() {
	s := sysinfo.Detect()
	rec := sysinfo.Recommend(s)
	gib := float64(s.RAMBytes) / (1024 * 1024 * 1024)
	fmt.Printf("System:\n")
	if s.RAMBytes > 0 {
		fmt.Printf("  RAM:        %.1f GiB\n", gib)
	} else {
		fmt.Printf("  RAM:        unknown (set EMBER_OLLAMA_MODEL manually)\n")
	}
	fmt.Printf("  CPUs:       %d\n", s.CPUs)
	if s.GPU != "" {
		fmt.Printf("  GPU:        %s\n", s.GPU)
	} else {
		fmt.Printf("  GPU:        none detected\n")
	}
	fmt.Printf("  OS:         %s\n\n", s.OS)
	if rec.DisableLLM {
		fmt.Printf("Recommendation: disable summaries (EMBER_DISABLE_SUMMARIES=1)\n")
		fmt.Printf("Reason: %s\n", rec.Reason)
		return
	}
	fmt.Printf("Recommended model: %s\n", rec.Model)
	fmt.Printf("Reason: %s\n\n", rec.Reason)
	fmt.Printf("To use it:\n")
	fmt.Printf("  export EMBER_OLLAMA_MODEL=%s\n", rec.Model)
	fmt.Printf("  (or set it in your compose env file)\n")
}

// warnDirectExposure logs a hardening warning when the process looks like it's
// exposed without the TLS-terminating reverse proxy it's designed to sit
// behind. ember serves plain HTTP only; the supported deployment puts Caddy in
// front. Bound to a non-loopback address with Secure cookies on and no trusted
// proxy configured, browsers will drop the (Secure) session cookie over plain
// HTTP and auth silently breaks — so surface it at startup rather than letting
// the operator chase a mystery login loop.
func warnDirectExposure(cfg config.Config, logger *slog.Logger) {
	if cfg.TestMode {
		return
	}
	host := cfg.Addr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	// Empty host (":8080") binds all interfaces; an explicit loopback address
	// (literal IP or the name "localhost") is the only clearly-safe case.
	// net.ParseIP("localhost") is nil, so check the name explicitly too.
	loopback := strings.EqualFold(host, "localhost") ||
		(host != "" && net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
	if loopback {
		return
	}
	if cfg.SecureCookies && len(cfg.TrustedProxies) == 0 {
		logger.Warn("ember binds a non-loopback address with Secure cookies and no EMBER_TRUSTED_PROXIES; "+
			"it serves plain HTTP and expects a TLS-terminating proxy (e.g. Caddy) in front. "+
			"If exposed directly over HTTP, browsers will drop the Secure session cookie and login will fail. "+
			"Put a TLS proxy in front (recommended), or set EMBER_SECURE_COOKIES=false for a deliberate plain-HTTP deployment.",
			"addr", cfg.Addr)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	api.Version = version
	logger.Info("ember starting", "version", version, "addr", cfg.Addr, "db", cfg.DBPath, "test_mode", cfg.TestMode)

	// Ensure DB directory exists when not in-memory.
	if cfg.DBPath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o750); err != nil {
			return fmt.Errorf("mkdir db: %w", err)
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	dbh, err := db.Open(ctx, cfg.DBPath)
	if err != nil {
		return fmt.Errorf("db open: %w", err)
	}
	defer dbh.Close()

	st := store.New(dbh)

	// One-shot backfill for articles inserted before the 0013 migration:
	// populates canonical_url + cluster_id so cross-feed dedup catches
	// historical rows too. Runs in a goroutine so server start isn't gated
	// on a large historical corpus; the dedup query gracefully skips rows
	// with cluster_id='' until the backfill catches up.
	st.BackfillClustersAsync(ctx, logger)

	sessionKey := cfg.SessionKey
	if sessionKey == "" && cfg.TestMode {
		// Test mode falls back to a hardcoded, publicly-known signing key so
		// e2e runs don't need a generated key. Anyone with this key can forge
		// session cookies — so this path must NEVER be hit in production. Warn
		// loudly; the operator should see it even at default log level.
		sessionKey = "00000000000000000000000000000000-ember-test-mode-key"
		logger.Warn("TEST MODE: using a hardcoded, publicly-known session signing key — " +
			"session cookies are forgeable. Never run EMBER_TEST_MODE in production. " +
			"Set EMBER_SESSION_KEY and unset EMBER_TEST_MODE for any real deployment.")
	}
	a, err := auth.New(st, sessionKey)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	// Cookie Secure flag: honor EMBER_SECURE_COOKIES (default true); test mode
	// always forces it off for plain-HTTP e2e runs.
	a.SecureCookies = cfg.SecureCookies
	if cfg.TestMode {
		a.SecureCookies = false
	}
	// Direct-exposure guardrail: the app serves plain HTTP and expects a
	// TLS-terminating proxy in front. Warn loudly if it looks like it's bound
	// to a public interface with Secure cookies on (→ browsers drop the cookie,
	// auth breaks) and no trusted proxy configured.
	warnDirectExposure(cfg, logger)
	// Apply operator-configured session lifetime if EMBER_SESSION_TTL was set.
	// Zero (cfg.SessionTTL not parsed) and out-of-range values fall through
	// to auth.DefaultSessionTTL — SetSessionTTL returns an error rather than
	// silently accepting bad values.
	if cfg.SessionTTL > 0 {
		if err := a.SetSessionTTL(cfg.SessionTTL); err != nil {
			logger.Warn("EMBER_SESSION_TTL rejected, using default",
				"requested", cfg.SessionTTL, "err", err,
				"min", auth.MinSessionTTL, "max", auth.MaxSessionTTL)
		}
	}
	// Admin-set session TTL persisted in app_settings overrides the env var
	// so changes via Settings → Sessions survive a restart. Same validation
	// as the HTTP handler — a hand-edited DB row with a 1-second TTL would
	// be rejected here instead of silently breaking session issuance.
	if v, _ := st.GetAppSetting(ctx, "session_ttl_seconds"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			if e := a.SetSessionTTL(time.Duration(n) * time.Second); e != nil {
				logger.Warn("session_ttl_seconds app_setting rejected",
					"seconds", n, "err", e,
					"min_seconds", int64(auth.MinSessionTTL.Seconds()),
					"max_seconds", int64(auth.MaxSessionTTL.Seconds()))
			} else {
				logger.Info("loaded session_ttl_seconds from app_settings", "seconds", n)
			}
		}
	}

	// WebAuthn (passkeys). Optional — requires EMBER_PUBLIC_URL so the relying
	// party ID + origin can be set. Passkey endpoints return 503 when absent.
	var webAuthn *auth.WebAuthn
	if cfg.PublicURL != "" {
		w, werr := auth.NewWebAuthn(st, cfg.PublicURL, "Ember")
		if werr != nil {
			logger.Warn("webauthn disabled", "err", werr)
		} else {
			webAuthn = w
			logger.Info("webauthn enabled", "rp", cfg.PublicURL)
		}
	}

	// Test mode seeds a deterministic admin + feed + articles so the e2e
	// harness has known data to assert against. In normal mode, do the
	// usual first-run admin bootstrap.
	if cfg.TestMode {
		if err := seedTestData(ctx, st, a, logger); err != nil {
			logger.Warn("test mode seed failed", "err", err)
		}
	} else if cfg.AdminUser != "" && cfg.AdminPassword != "" {
		u, created, err := a.BootstrapAdmin(ctx, cfg.AdminUser, cfg.AdminPassword)
		if err != nil {
			logger.Warn("bootstrap admin failed", "err", err)
		} else if created {
			logger.Warn("created first-run admin — change the password!", "user", u.Username)
		}
	}

	op := opml.NewService(st)
	op.ValidateURL = func(ctx context.Context, raw string) error {
		return urlcheck.Check(ctx, raw, cfg.AllowPrivateURLs)
	}
	tt := ttrss.NewService(st)
	tt.ValidateURL = func(ctx context.Context, raw string) error {
		return urlcheck.Check(ctx, raw, cfg.AllowPrivateURLs)
	}

	// Summarizer: noop in test mode, nil if disabled at install, otherwise
	// Ollama. The active model is the persisted app setting if present, else
	// the env-var default — so admin model switches survive a restart.
	var sum summarize.Summarizer
	var ollamaSum *summarize.Ollama
	switch {
	case cfg.DisableSummaries:
		logger.Info("AI summaries disabled via EMBER_DISABLE_SUMMARIES")
		sum = nil
	case cfg.TestMode:
		sum = summarize.Noop{}
	default:
		model := cfg.OllamaModel
		if saved, _ := st.GetAppSetting(ctx, "ollama_model"); saved != "" {
			model = saved
			logger.Info("loaded saved ollama_model from app_settings", "model", model)
		}
		ollamaSum = summarize.NewOllama(cfg.OllamaURL, model)
		// Load any persisted generation tunables so they survive a restart.
		opts := summarize.Options{}
		if v, _ := st.GetAppSetting(ctx, "llm_temperature"); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				opts.Temperature = f
			}
		}
		if v, _ := st.GetAppSetting(ctx, "llm_top_p"); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				opts.TopP = f
			}
		}
		if v, _ := st.GetAppSetting(ctx, "llm_num_ctx"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				opts.NumCtx = n
			}
		}
		ollamaSum.SetOptions(opts)
		sum = ollamaSum
	}

	fetcher := feed.NewFetcher(30 * time.Second)
	// Block redirects to private/internal addresses on every feed fetch.
	fetcher.Client.CheckRedirect = feed.RedirectGuard(func(rawURL string) error {
		return urlcheck.Check(ctx, rawURL, cfg.AllowPrivateURLs)
	})
	p := poller.New(st, fetcher, sum, poller.Config{
		Tick:                        cfg.PollTick,
		Concurrency:                 cfg.PollConcurrency,
		SummaryWorker:               !cfg.TestMode && !cfg.DisableSummaries,
		EnrichOnIngest:              !cfg.TestMode,
		DisableImages:               cfg.DisableImages,
		AllowPrivateURLs:            cfg.AllowPrivateURLs,
		InitialBacklogHoursFallback: store.DefaultInitialBacklogHours,
	}, logger.With("component", "poller"))

	// Background workers are tracked in a WaitGroup so shutdown can wait for an
	// in-flight DB backup / digest send to finish its current iteration instead
	// of the process exiting mid-write. Each worker already returns on ctx.Done().
	var bgWG sync.WaitGroup
	runBG := func(fn func()) {
		bgWG.Add(1)
		go func() {
			defer bgWG.Done()
			fn()
		}()
	}

	// Background poller. Skipped in test mode — articles are pre-seeded and
	// the fake feed URL doesn't resolve.
	if !cfg.TestMode {
		runBG(func() { p.Run(ctx) })
		// Scheduled DB maintenance: a single goroutine that ticks every hour
		// and runs the backup / cleanup actions when their app_setting cadence
		// says it's time. Failures log and continue.
		runBG(func() { runDBMaintenance(ctx, st, op, logger.With("component", "db-maintenance")) })
		// Daily digest sender. Always runs; each tick resolves the live SMTP
		// config from app_settings overlaid on the env-derived fallback, so
		// admins can configure SMTP via Settings without restarting. When
		// SMTP isn't configured (no host/port/from), the tick is a no-op.
		smtpFallback := store.SMTPSettings{
			Host: cfg.SMTPHost, Port: cfg.SMTPPort,
			Username: cfg.SMTPUser, Password: cfg.SMTPPassword,
			From: cfg.SMTPFrom, StartTLS: cfg.SMTPStartTLS,
		}
		sender := &digest.Sender{Store: st}
		runBG(func() { runDigestSender(ctx, st, sender, smtpFallback, logger.With("component", "digest")) })
		// Reap stale WebAuthn ceremony rows (created_at < now-5m). Cheap.
		runBG(func() {
			t := time.NewTicker(15 * time.Minute)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					_ = st.CleanupWebAuthnSessions(ctx)
				}
			}
		})
		// Reap expired sessions hourly. Cookies are deleted lazily on access,
		// but never-revisited rows (logged-out browsers, rotated cookies)
		// would otherwise accumulate forever.
		runBG(func() {
			t := time.NewTicker(1 * time.Hour)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if n, err := a.CleanupExpiredSessions(ctx); err != nil {
						logger.Warn("session cleanup failed", "err", err)
					} else if n > 0 {
						logger.Info("expired sessions reaped", "deleted", n)
					}
				}
			}
		})
	}

	// Embedded static SPA.
	staticH, err := web.Handler()
	if err != nil {
		logger.Warn("embedded SPA unavailable; serving without it", "err", err)
	}

	router := api.NewRouter(api.Dependencies{
		Store: st, Auth: a, Poller: p, Metrics: p, OPML: op, TTRSS: tt,
		StaticH: staticH, TestMode: cfg.TestMode, Ollama: ollamaSum,
		WebAuthn: webAuthn,
		// Test mode uses synthetic .test hostnames that don't resolve; the
		// SSRF DNS check would reject them. Production stays strict.
		AllowPrivateURLs: cfg.AllowPrivateURLs || cfg.TestMode,
		// CIDRs whose X-Real-IP / X-Forwarded-Proto we trust. Empty = the app is
		// the edge (trust the connection peer, ignore the headers).
		TrustedProxies: cfg.TrustedProxies,
		// FreshWindow makes EMBER_FRESH_WINDOW actually take effect — the
		// Fresh-view article list, the sidebar's Fresh count, and the
		// client-side isFresh() all read from this single source.
		FreshWindow: cfg.FreshWindow,
		// Env-derived SMTP and backlog defaults. The /api/admin/settings
		// endpoints overlay app_settings rows on these so an admin can
		// change them at runtime without restarting.
		SMTPFallback: store.SMTPSettings{
			Host: cfg.SMTPHost, Port: cfg.SMTPPort,
			Username: cfg.SMTPUser, Password: cfg.SMTPPassword,
			From: cfg.SMTPFrom, StartTLS: cfg.SMTPStartTLS,
		},
		InitialBacklogHoursFallback: store.DefaultInitialBacklogHours,
		// Handlers that spawn detached goroutines (initial feed refresh on
		// starter-pack import / add-feed) derive their context from this
		// parent so they don't outlive process shutdown and end up making
		// DB calls against a closed handle.
		BackgroundCtx: ctx,
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       120 * time.Second,
		// Cap request headers at 64 KiB (stdlib default is 1 MiB). Without a
		// fronting proxy to buffer/limit, this bounds header-bomb memory; no
		// legitimate request needs anywhere near this.
		MaxHeaderBytes: 64 << 10,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancelSh := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelSh()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("http shutdown error", "err", err)
	}

	// Wait for background workers to finish their current iteration (e.g. an
	// in-flight DB backup / VACUUM or digest send) before exiting, bounded so a
	// stuck worker can't hang shutdown forever.
	bgDone := make(chan struct{})
	go func() {
		bgWG.Wait()
		close(bgDone)
	}()
	select {
	case <-bgDone:
	case <-time.After(15 * time.Second):
		logger.Warn("background workers did not stop within timeout")
	}

	logger.Info("ember stopped")
	return nil
}
