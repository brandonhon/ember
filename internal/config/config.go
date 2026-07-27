// Package config loads ember's runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
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
	// PollMinInterval is the floor for the adaptive per-feed fetch interval
	// ("check feeds every…"). Default 30m. Override via EMBER_POLL_MIN_INTERVAL;
	// admins can also change it at runtime in Settings (app_settings overrides
	// this). Validated to [5m, 24h].
	PollMinInterval time.Duration
	// SessionTTL is the session idle timeout: the maximum gap between requests
	// before a session expires. Active sessions slide forward on each request,
	// capped at 30 days from login (auth.DefaultMaxSessionLifetime). Defaults
	// to 24h. Override via EMBER_SESSION_TTL (Go duration: e.g. 30m, 12h, 7d
	// not supported — use 168h for a week).
	SessionTTL time.Duration
	LogLevel   slog.Level
	TestMode   bool
	// DisableSummaries skips the LLM summarizer entirely. Articles still show
	// in lists; the UI renders the article body without a summary card.
	DisableSummaries bool
	// DisableImages drops image_url at ingest, so no main image gets stored or
	// shown. Per-user UI prefs further hide images at display time.
	DisableImages bool
	// DisableUpdateCheck turns off the background GitHub-releases update check.
	// It sets the boot-time default for the update_check_enabled admin setting,
	// which an admin can still toggle at runtime via Settings.
	DisableUpdateCheck bool
	// PasskeyRequireUV makes passkey sign-in demand user verification
	// (PIN/biometric) instead of merely preferring it. Sets the boot-time
	// default for the passkey_require_uv admin setting. Off by default: a
	// passkey already enrolled on a PIN-less security key would stop working.
	PasskeyRequireUV bool
	// AllowPrivateURLs disables the SSRF block on outbound HTTP fetches so a
	// homelab can subscribe to feeds on its LAN. Default false (production).
	AllowPrivateURLs bool
	// HSTSPreload appends "; preload" to the Strict-Transport-Security header.
	// Only enable after verifying the domain is (or will be) submitted to the
	// HSTS preload list — it is a long-term, browser-level commitment.
	// Set EMBER_HSTS_PRELOAD=true to enable.
	HSTSPreload bool
	// SMTP for daily digest emails. Configured = host + port + from. Username
	// + password are optional (skipped when empty).
	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string
	SMTPFrom     string
	SMTPStartTLS bool
	// Email inbox (inbound newsletter feature). When EmailDomain is
	// empty the SMTP listener doesn't start and the inbox endpoints
	// return enabled=false. EmailListenAddr defaults to :2525; operators
	// fronting the bind via Caddy / haproxy can pick another port.
	EmailDomain     string
	EmailListenAddr string
	EmailMaxBytes   int64
	// PublicURL is the canonical scheme://host[:port] users hit the app on.
	// Required for WebAuthn registration so the RP ID + origin can be set.
	// Optional otherwise.
	PublicURL string
	// SecureCookies sets the Secure flag on the session + CSRF cookies.
	// Defaults true (the app expects TLS, normally terminated by a fronting
	// proxy). Set EMBER_SECURE_COOKIES=false ONLY for a deliberate plain-HTTP
	// deployment (e.g. behind a VPN) — otherwise browsers drop Secure cookies
	// over HTTP and auth silently breaks. Forced false in test mode.
	SecureCookies bool
	// TrustedProxies is the set of CIDRs whose X-Real-IP / X-Forwarded-Proto
	// headers ember will trust (for rate-limit keying and HTTPS detection).
	// Empty = trust nobody: the app is the edge and reads the real peer from
	// the connection. Set EMBER_TRUSTED_PROXIES to the fronting proxy's address
	// (e.g. the Caddy container IP/range) when deployed behind one. Comma- or
	// space-separated CIDRs or bare IPs.
	TrustedProxies []string
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
		PollMinInterval: 30 * time.Minute,
		SessionTTL:      24 * time.Hour,
		LogLevel:        slog.LevelInfo,
		SMTPPort:        587,
		SMTPStartTLS:    true,
		SecureCookies:   true,
		EmailListenAddr: ":2525",
		EmailMaxBytes:   25 * 1024 * 1024,
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

// env reads configuration variables, accumulating problems instead of failing
// on the first one so an operator sees every bad value in a single boot log.
// Every reader leaves the destination untouched when the variable is unset or
// empty, which is what preserves the Defaults() value.
type env struct {
	get  func(string) string
	errs []string
}

// fail records a problem against a variable. Messages always start with the
// variable name — it is the only context an operator gets at boot.
func (e *env) fail(key, format string, args ...any) {
	e.errs = append(e.errs, key+" "+fmt.Sprintf(format, args...))
}

// str assigns the raw value when set.
func (e *env) str(key string, dst *string) {
	if v := e.get(key); v != "" {
		*dst = v
	}
}

// boolean accepts 1/true/yes/on and 0/false/no/off, case-insensitively.
func (e *env) boolean(key string, dst *bool) {
	v := e.get(key)
	if v == "" {
		return
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		*dst = true
	case "0", "false", "no", "off":
		*dst = false
	default:
		e.fail(key, "invalid bool %q", v)
	}
}

// duration parses a Go duration and enforces the range. lo == 0 means "any
// positive value"; hi == 0 means "no upper bound".
func (e *env) duration(key string, dst *time.Duration, lo, hi time.Duration) {
	v := e.get(key)
	if v == "" {
		return
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		e.fail(key, "invalid: %v", err)
		return
	}
	if d <= 0 || d < lo || (hi > 0 && d > hi) {
		switch {
		case lo == 0 && hi == 0:
			e.fail(key, "must be > 0")
		case hi == 0:
			e.fail(key, "must be >= %s", lo)
		default:
			e.fail(key, "must be between %s and %s", lo, hi)
		}
		return
	}
	*dst = d
}

// number parses a signed integer and enforces lo <= n <= hi; hi == 0 means "no
// upper bound". The comparison happens in int64 before narrowing, so a value
// that would overflow T is rejected rather than silently truncated. A method
// can't carry type parameters, hence a function over *env.
func number[T int | int64](e *env, key string, dst *T, lo, hi T) {
	v := e.get(key)
	if v == "" {
		return
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		e.fail(key, "invalid: %v", err)
		return
	}
	if n < int64(lo) || (hi > 0 && n > int64(hi)) {
		if hi > 0 {
			e.fail(key, "must be between %d and %d", lo, hi)
		} else {
			e.fail(key, "must be >= %d", lo)
		}
		return
	}
	*dst = T(n)
}

// Bounds for EMBER_POLL_MIN_INTERVAL. These mirror store.PollMinInterval{Floor,
// Ceil}; kept as literals so config stays free of a store import.
const (
	pollMinIntervalFloor = 5 * time.Minute
	pollMinIntervalCeil  = 24 * time.Hour
)

func loadFrom(get func(string) string) (Config, error) {
	cfg := Defaults()
	e := &env{get: get}

	e.str("EMBER_ADDR", &cfg.Addr)
	e.str("EMBER_DB_PATH", &cfg.DBPath)
	e.str("EMBER_ADMIN_USER", &cfg.AdminUser)
	e.str("EMBER_OLLAMA_MODEL", &cfg.OllamaModel)
	e.str("EMBER_PUBLIC_URL", &cfg.PublicURL)
	e.str("EMBER_SMTP_HOST", &cfg.SMTPHost)
	e.str("EMBER_SMTP_USER", &cfg.SMTPUser)
	e.str("EMBER_SMTP_PASSWORD", &cfg.SMTPPassword)
	e.str("EMBER_SMTP_FROM", &cfg.SMTPFrom)
	e.str("EMBER_EMAIL_DOMAIN", &cfg.EmailDomain)
	e.str("EMBER_EMAIL_LISTEN_ADDR", &cfg.EmailListenAddr)

	// Secrets are read raw: an empty value is meaningful (it triggers the
	// required-key check below) rather than "leave the default".
	cfg.SessionKey = get("EMBER_SESSION_KEY")
	cfg.AdminPassword = get("EMBER_ADMIN_PASSWORD")

	e.duration("EMBER_FRESH_WINDOW", &cfg.FreshWindow, 0, 0)
	e.duration("EMBER_SESSION_TTL", &cfg.SessionTTL, 0, 0)
	e.duration("EMBER_POLL_TICK", &cfg.PollTick, 0, 0)
	e.duration("EMBER_POLL_MIN_INTERVAL", &cfg.PollMinInterval, pollMinIntervalFloor, pollMinIntervalCeil)

	number(e, "EMBER_POLL_CONCURRENCY", &cfg.PollConcurrency, 1, 0)
	number(e, "EMBER_SMTP_PORT", &cfg.SMTPPort, 1, 65535)
	number(e, "EMBER_EMAIL_MAX_BYTES", &cfg.EmailMaxBytes, 1, 0)

	e.boolean("EMBER_TEST_MODE", &cfg.TestMode)
	e.boolean("EMBER_DISABLE_SUMMARIES", &cfg.DisableSummaries)
	e.boolean("EMBER_DISABLE_IMAGES", &cfg.DisableImages)
	e.boolean("EMBER_DISABLE_UPDATE_CHECK", &cfg.DisableUpdateCheck)
	e.boolean("EMBER_PASSKEY_REQUIRE_UV", &cfg.PasskeyRequireUV)
	e.boolean("EMBER_SECURE_COOKIES", &cfg.SecureCookies)
	e.boolean("EMBER_ALLOW_PRIVATE_URLS", &cfg.AllowPrivateURLs)
	e.boolean("EMBER_HSTS_PRELOAD", &cfg.HSTSPreload)
	e.boolean("EMBER_SMTP_STARTTLS", &cfg.SMTPStartTLS)

	// The remaining three parse into non-primitive shapes.
	if v := get("EMBER_OLLAMA_URL"); v != "" {
		u, parseErr := url.Parse(v)
		switch {
		case parseErr != nil:
			e.fail("EMBER_OLLAMA_URL", "invalid: %v", parseErr)
		case u.Scheme != "http" && u.Scheme != "https":
			e.fail("EMBER_OLLAMA_URL", "must use http or https scheme, got %q", u.Scheme)
		default:
			cfg.OllamaURL = v
		}
	}
	if v := get("EMBER_LOG_LEVEL"); v != "" {
		lvl, err := parseLogLevel(v)
		if err != nil {
			e.fail("EMBER_LOG_LEVEL", "invalid: %q", v)
		} else {
			cfg.LogLevel = lvl
		}
	}
	if v := get("EMBER_TRUSTED_PROXIES"); v != "" {
		proxies, err := parseProxyList(v)
		if err != nil {
			e.fail("EMBER_TRUSTED_PROXIES", "%v", err)
		} else {
			cfg.TrustedProxies = proxies
		}
	}

	if !cfg.TestMode && cfg.SessionKey == "" {
		e.errs = append(e.errs, "EMBER_SESSION_KEY is required (32+ bytes)")
	}
	if cfg.SessionKey != "" && len(cfg.SessionKey) < 32 {
		e.errs = append(e.errs, "EMBER_SESSION_KEY must be at least 32 bytes")
	}

	if len(e.errs) > 0 {
		return cfg, errors.New(strings.Join(e.errs, "; "))
	}
	return cfg, nil
}

// parseProxyList parses a comma/space-separated list of CIDRs or bare IPs into
// canonical CIDR strings. A bare IPv4 becomes /32, a bare IPv6 /128. Returns an
// error on any unparseable entry so a typo'd proxy address fails loudly rather
// than silently trusting nobody (which would mis-key the rate limiter).
func parseProxyList(v string) ([]string, error) {
	fields := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if strings.Contains(f, "/") {
			if _, _, err := net.ParseCIDR(f); err != nil {
				return nil, fmt.Errorf("invalid CIDR %q: %v", f, err)
			}
			out = append(out, f)
			continue
		}
		ip := net.ParseIP(f)
		if ip == nil {
			return nil, fmt.Errorf("invalid IP %q", f)
		}
		if ip.To4() != nil {
			out = append(out, f+"/32")
		} else {
			out = append(out, f+"/128")
		}
	}
	return out, nil
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
		return slog.LevelInfo, fmt.Errorf("invalid log level %q", v)
	}
}
