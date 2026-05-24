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
	"syscall"
	"time"

	"github.com/brandonhon/ember/internal/api"
	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/config"
	"github.com/brandonhon/ember/internal/db"
	"github.com/brandonhon/ember/internal/feed"
	"github.com/brandonhon/ember/internal/opml"
	"github.com/brandonhon/ember/internal/poller"
	"github.com/brandonhon/ember/internal/store"
	"github.com/brandonhon/ember/internal/summarize"
	"github.com/brandonhon/ember/internal/web"
)

// version is set via -ldflags at build time.
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-v" || os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Printf("ember %s\n", version)
		return
	}
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "ember: %v\n", err)
		os.Exit(1)
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

	// Summarizer: noop in test mode, nil if disabled at install, otherwise Ollama.
	var sum summarize.Summarizer
	switch {
	case cfg.DisableSummaries:
		logger.Info("AI summaries disabled via EMBER_DISABLE_SUMMARIES")
		sum = nil
	case cfg.TestMode:
		sum = summarize.Noop{}
	default:
		sum = summarize.NewOllama(cfg.OllamaURL, cfg.OllamaModel)
	}

	fetcher := feed.NewFetcher(30 * time.Second)
	p := poller.New(st, fetcher, sum, poller.Config{
		Tick:           cfg.PollTick,
		Concurrency:    cfg.PollConcurrency,
		SummaryWorker:  !cfg.TestMode && !cfg.DisableSummaries,
		EnrichOnIngest: !cfg.TestMode,
		DisableImages:  cfg.DisableImages,
	}, logger.With("component", "poller"))

	// Background poller. Skipped in test mode — articles are pre-seeded and
	// the fake feed URL doesn't resolve.
	if !cfg.TestMode {
		go p.Run(ctx)
	}

	// Embedded static SPA.
	staticH, err := web.Handler()
	if err != nil {
		logger.Warn("embedded SPA unavailable; serving without it", "err", err)
	}

	router := api.NewRouter(api.Dependencies{
		Store: st, Auth: a, Poller: p, Metrics: p, OPML: op,
		StaticH: staticH, TestMode: cfg.TestMode,
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
