// Package config loads ember's runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration for ember. Populated once at startup.
type Config struct {
	Addr            string
	DBPath          string
	SessionKey      string
	AdminUser       string
	AdminPassword   string
	OllamaURL       string
	OllamaModel     string
	FreshWindow     time.Duration
	PollConcurrency int
	PollTick        time.Duration
	LogLevel        slog.Level
	TestMode        bool
	// DisableSummaries skips the LLM summarizer entirely. Articles still show
	// in lists; the UI renders the article body without a summary card.
	DisableSummaries bool
	// DisableImages drops image_url at ingest, so no main image gets stored or
	// shown. Per-user UI prefs further hide images at display time.
	DisableImages bool
	// AllowPrivateURLs disables the SSRF block on outbound HTTP fetches so a
	// homelab can subscribe to feeds on its LAN. Default false (production).
	AllowPrivateURLs bool
}

// Defaults returns a Config populated with safe defaults. SessionKey and
// AdminPassword have no defaults — Load returns an error if they are required
// but missing.
func Defaults() Config {
	return Config{
		Addr:            ":8080",
		DBPath:          "/data/ember.db",
		AdminUser:       "admin",
		OllamaURL:       "http://ollama:11434",
		OllamaModel:     "qwen2.5:0.5b",
		FreshWindow:     6 * time.Hour,
		PollConcurrency: 8,
		PollTick:        60 * time.Second,
		LogLevel:        slog.LevelInfo,
	}
}

// Load reads configuration from environment variables. Required variables that
// are missing cause an error in non-test mode.
func Load() (Config, error) {
	return loadFrom(os.Getenv)
}

// LoadFromMap is a test helper that reads from a map instead of the process
// environment.
func LoadFromMap(env map[string]string) (Config, error) {
	return loadFrom(func(k string) string { return env[k] })
}

func loadFrom(get func(string) string) (Config, error) {
	cfg := Defaults()
	var errs []string

	if v := get("EMBER_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := get("EMBER_DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	cfg.SessionKey = get("EMBER_SESSION_KEY")
	if v := get("EMBER_ADMIN_USER"); v != "" {
		cfg.AdminUser = v
	}
	cfg.AdminPassword = get("EMBER_ADMIN_PASSWORD")
	if v := get("EMBER_OLLAMA_URL"); v != "" {
		cfg.OllamaURL = v
	}
	if v := get("EMBER_OLLAMA_MODEL"); v != "" {
		cfg.OllamaModel = v
	}
	if v := get("EMBER_FRESH_WINDOW"); v != "" {
		d, err := time.ParseDuration(v)
		switch {
		case err != nil:
			errs = append(errs, fmt.Sprintf("EMBER_FRESH_WINDOW invalid: %v", err))
		case d <= 0:
			errs = append(errs, "EMBER_FRESH_WINDOW must be > 0")
		default:
			cfg.FreshWindow = d
		}
	}
	if v := get("EMBER_POLL_CONCURRENCY"); v != "" {
		n, err := strconv.Atoi(v)
		switch {
		case err != nil:
			errs = append(errs, fmt.Sprintf("EMBER_POLL_CONCURRENCY invalid: %v", err))
		case n < 1:
			errs = append(errs, "EMBER_POLL_CONCURRENCY must be >= 1")
		default:
			cfg.PollConcurrency = n
		}
	}
	if v := get("EMBER_POLL_TICK"); v != "" {
		d, err := time.ParseDuration(v)
		switch {
		case err != nil:
			errs = append(errs, fmt.Sprintf("EMBER_POLL_TICK invalid: %v", err))
		case d <= 0:
			errs = append(errs, "EMBER_POLL_TICK must be > 0")
		default:
			cfg.PollTick = d
		}
	}
	if v := get("EMBER_LOG_LEVEL"); v != "" {
		lvl, err := parseLogLevel(v)
		if err != nil {
			errs = append(errs, err.Error())
		} else {
			cfg.LogLevel = lvl
		}
	}
	if v := get("EMBER_TEST_MODE"); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			cfg.TestMode = true
		case "0", "false", "no", "off", "":
			cfg.TestMode = false
		default:
			errs = append(errs, fmt.Sprintf("EMBER_TEST_MODE invalid: %q", v))
		}
	}
	if v := get("EMBER_DISABLE_SUMMARIES"); v != "" {
		on, err := parseBool(v)
		if err != nil {
			errs = append(errs, fmt.Sprintf("EMBER_DISABLE_SUMMARIES %v", err))
		} else {
			cfg.DisableSummaries = on
		}
	}
	if v := get("EMBER_DISABLE_IMAGES"); v != "" {
		on, err := parseBool(v)
		if err != nil {
			errs = append(errs, fmt.Sprintf("EMBER_DISABLE_IMAGES %v", err))
		} else {
			cfg.DisableImages = on
		}
	}
	if v := get("EMBER_ALLOW_PRIVATE_URLS"); v != "" {
		on, err := parseBool(v)
		if err != nil {
			errs = append(errs, fmt.Sprintf("EMBER_ALLOW_PRIVATE_URLS %v", err))
		} else {
			cfg.AllowPrivateURLs = on
		}
	}

	if !cfg.TestMode && cfg.SessionKey == "" {
		errs = append(errs, "EMBER_SESSION_KEY is required (32+ bytes)")
	}
	if cfg.SessionKey != "" && len(cfg.SessionKey) < 32 {
		errs = append(errs, "EMBER_SESSION_KEY must be at least 32 bytes")
	}

	if len(errs) > 0 {
		return cfg, errors.New(strings.Join(errs, "; "))
	}
	return cfg, nil
}

func parseBool(v string) (bool, error) {
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool %q", v)
	}
}

func parseLogLevel(v string) (slog.Level, error) {
	switch strings.ToLower(v) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("EMBER_LOG_LEVEL invalid: %q", v)
	}
}
