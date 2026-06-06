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
	// SessionTTL is the lifetime of a freshly-issued session cookie. Defaults
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
	// AllowPrivateURLs disables the SSRF block on outbound HTTP fetches so a
	// homelab can subscribe to feeds on its LAN. Default false (production).
	AllowPrivateURLs bool
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
		SessionTTL:      24 * time.Hour,
		LogLevel:        slog.LevelInfo,
		SMTPPort:        587,
		SMTPStartTLS:    true,
		SecureCookies:   true,
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
		u, parseErr := url.Parse(v)
		switch {
		case parseErr != nil:
			errs = append(errs, fmt.Sprintf("EMBER_OLLAMA_URL invalid: %v", parseErr))
		case u.Scheme != "http" && u.Scheme != "https":
			errs = append(errs, fmt.Sprintf("EMBER_OLLAMA_URL must use http or https scheme, got %q", u.Scheme))
		default:
			cfg.OllamaURL = v
		}
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
	if v := get("EMBER_SESSION_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		switch {
		case err != nil:
			errs = append(errs, fmt.Sprintf("EMBER_SESSION_TTL invalid: %v", err))
		case d <= 0:
			errs = append(errs, "EMBER_SESSION_TTL must be > 0")
		default:
			cfg.SessionTTL = d
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
	if v := get("EMBER_PUBLIC_URL"); v != "" {
		cfg.PublicURL = v
	}
	if v := get("EMBER_SECURE_COOKIES"); v != "" {
		on, err := parseBool(v)
		if err != nil {
			errs = append(errs, fmt.Sprintf("EMBER_SECURE_COOKIES %v", err))
		} else {
			cfg.SecureCookies = on
		}
	}
	if v := get("EMBER_TRUSTED_PROXIES"); v != "" {
		proxies, err := parseProxyList(v)
		if err != nil {
			errs = append(errs, fmt.Sprintf("EMBER_TRUSTED_PROXIES %v", err))
		} else {
			cfg.TrustedProxies = proxies
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
	if v := get("EMBER_SMTP_HOST"); v != "" {
		cfg.SMTPHost = v
	}
	if v := get("EMBER_SMTP_PORT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 65535 {
			errs = append(errs, "EMBER_SMTP_PORT invalid")
		} else {
			cfg.SMTPPort = n
		}
	}
	if v := get("EMBER_SMTP_USER"); v != "" {
		cfg.SMTPUser = v
	}
	if v := get("EMBER_SMTP_PASSWORD"); v != "" {
		cfg.SMTPPassword = v
	}
	if v := get("EMBER_SMTP_FROM"); v != "" {
		cfg.SMTPFrom = v
	}
	if v := get("EMBER_SMTP_STARTTLS"); v != "" {
		on, err := parseBool(v)
		if err != nil {
			errs = append(errs, fmt.Sprintf("EMBER_SMTP_STARTTLS %v", err))
		} else {
			cfg.SMTPStartTLS = on
		}
	}
	// Inbound email-inbox feature.
	if v := get("EMBER_EMAIL_DOMAIN"); v != "" {
		cfg.EmailDomain = v
	}
	cfg.EmailListenAddr = ":2525"
	if v := get("EMBER_EMAIL_LISTEN_ADDR"); v != "" {
		cfg.EmailListenAddr = v
	}
	cfg.EmailMaxBytes = 25 * 1024 * 1024
	if v := get("EMBER_EMAIL_MAX_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			errs = append(errs, "EMBER_EMAIL_MAX_BYTES invalid")
		} else {
			cfg.EmailMaxBytes = n
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
