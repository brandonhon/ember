// ember is a self-hosted RSS/Atom reader. The binary embeds a Svelte SPA,
// runs the JSON API + Fever shim, and runs a background poller.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
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

	sessionKey := cfg.SessionKey
	if sessionKey == "" && cfg.TestMode {
		sessionKey = "00000000000000000000000000000000-ember-test-mode-key"
	}
	a, err := auth.New(st, sessionKey)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	if cfg.TestMode {
		a.SecureCookies = false
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
		Tick:             cfg.PollTick,
		Concurrency:      cfg.PollConcurrency,
		SummaryWorker:    !cfg.TestMode && !cfg.DisableSummaries,
		EnrichOnIngest:   !cfg.TestMode,
		DisableImages:    cfg.DisableImages,
		AllowPrivateURLs: cfg.AllowPrivateURLs,
	}, logger.With("component", "poller"))

	// Background poller. Skipped in test mode — articles are pre-seeded and
	// the fake feed URL doesn't resolve.
	if !cfg.TestMode {
		go p.Run(ctx)
		// Scheduled DB maintenance: a single goroutine that ticks every hour
		// and runs the backup / cleanup actions when their app_setting cadence
		// says it's time. Failures log and continue.
		go runDBMaintenance(ctx, st, op, logger.With("component", "db-maintenance"))
		// Daily digest sender. Skipped when SMTP isn't configured.
		sender := &digest.Sender{
			Store: st,
			SMTP: digest.SMTPConfig{
				Host: cfg.SMTPHost, Port: cfg.SMTPPort,
				Username: cfg.SMTPUser, Password: cfg.SMTPPassword,
				From: cfg.SMTPFrom, StartTLS: cfg.SMTPStartTLS,
			},
		}
		if sender.SMTP.Configured() {
			go runDigestSender(ctx, st, sender, logger.With("component", "digest"))
		}
		// Reap stale WebAuthn ceremony rows (created_at < now-5m). Cheap.
		go func() {
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
		}()
		// Reap expired sessions hourly. Cookies are deleted lazily on access,
		// but never-revisited rows (logged-out browsers, rotated cookies)
		// would otherwise accumulate forever.
		go func() {
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
		}()
	}

	// Embedded static SPA.
	staticH, err := web.Handler()
	if err != nil {
		logger.Warn("embedded SPA unavailable; serving without it", "err", err)
	}

	router := api.NewRouter(api.Dependencies{
		Store: st, Auth: a, Poller: p, Metrics: p, OPML: op,
		StaticH: staticH, TestMode: cfg.TestMode, Ollama: ollamaSum,
		WebAuthn: webAuthn,
		// Test mode uses synthetic .test hostnames that don't resolve; the
		// SSRF DNS check would reject them. Production stays strict.
		AllowPrivateURLs: cfg.AllowPrivateURLs || cfg.TestMode,
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
	logger.Info("ember stopped")
	return nil
}
