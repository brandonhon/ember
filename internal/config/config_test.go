package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	d := Defaults()
	if d.Addr != ":8080" {
		t.Errorf("default Addr = %q, want :8080", d.Addr)
	}
	if d.DBPath != "/data/ember.db" {
		t.Errorf("default DBPath = %q", d.DBPath)
	}
	if d.OllamaURL != "http://ollama:11434" {
		t.Errorf("default OllamaURL = %q", d.OllamaURL)
	}
	if d.OllamaModel != "qwen2.5:0.5b" {
		t.Errorf("default OllamaModel = %q", d.OllamaModel)
	}
	if d.FreshWindow != 6*time.Hour {
		t.Errorf("default FreshWindow = %v, want 6h", d.FreshWindow)
	}
	if d.PollConcurrency != 8 {
		t.Errorf("default PollConcurrency = %d, want 8", d.PollConcurrency)
	}
	if d.PollTick != 60*time.Second {
		t.Errorf("default PollTick = %v, want 60s", d.PollTick)
	}
	if d.LogLevel != slog.LevelInfo {
		t.Errorf("default LogLevel = %v, want info", d.LogLevel)
	}
	if !d.SecureCookies {
		t.Error("default SecureCookies = false, want true")
	}
}

func TestLoad_DefaultsWithSessionKey(t *testing.T) {
	cfg, err := LoadFromMap(map[string]string{
		"EMBER_SESSION_KEY": strings.Repeat("a", 32),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("Addr = %q, want default :8080", cfg.Addr)
	}
	if cfg.OllamaModel != "qwen2.5:0.5b" {
		t.Errorf("OllamaModel = %q, want default", cfg.OllamaModel)
	}
}

func TestLoad_OverridesApplied(t *testing.T) {
	cfg, err := LoadFromMap(map[string]string{
		"EMBER_SESSION_KEY":       strings.Repeat("k", 32),
		"EMBER_ADDR":              ":9090",
		"EMBER_DB_PATH":           "/tmp/x.db",
		"EMBER_OLLAMA_URL":        "http://localhost:11434",
		"EMBER_OLLAMA_MODEL":      "llama3.2:1b",
		"EMBER_FRESH_WINDOW":      "12h",
		"EMBER_POLL_CONCURRENCY":  "4",
		"EMBER_POLL_TICK":         "30s",
		"EMBER_POLL_MIN_INTERVAL": "45m",
		"EMBER_LOG_LEVEL":         "debug",
		"EMBER_ADMIN_USER":        "root",
		"EMBER_ADMIN_PASSWORD":    "secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Addr != ":9090" {
		t.Errorf("Addr = %q", cfg.Addr)
	}
	if cfg.DBPath != "/tmp/x.db" {
		t.Errorf("DBPath = %q", cfg.DBPath)
	}
	if cfg.OllamaModel != "llama3.2:1b" {
		t.Errorf("OllamaModel = %q", cfg.OllamaModel)
	}
	if cfg.FreshWindow != 12*time.Hour {
		t.Errorf("FreshWindow = %v", cfg.FreshWindow)
	}
	if cfg.PollConcurrency != 4 {
		t.Errorf("PollConcurrency = %d", cfg.PollConcurrency)
	}
	if cfg.PollTick != 30*time.Second {
		t.Errorf("PollTick = %v", cfg.PollTick)
	}
	if cfg.PollMinInterval != 45*time.Minute {
		t.Errorf("PollMinInterval = %v", cfg.PollMinInterval)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v", cfg.LogLevel)
	}
	if cfg.AdminUser != "root" || cfg.AdminPassword != "secret" {
		t.Errorf("admin = %q/%q", cfg.AdminUser, cfg.AdminPassword)
	}
}

func TestLoad_PollMinIntervalRange(t *testing.T) {
	base := map[string]string{"EMBER_SESSION_KEY": strings.Repeat("k", 32)}
	// Default applies when unset.
	if cfg, err := LoadFromMap(base); err != nil || cfg.PollMinInterval != 30*time.Minute {
		t.Fatalf("default PollMinInterval = %v, err %v; want 30m", cfg.PollMinInterval, err)
	}
	for _, bad := range []string{"1m", "48h", "0s"} {
		m := map[string]string{"EMBER_SESSION_KEY": strings.Repeat("k", 32), "EMBER_POLL_MIN_INTERVAL": bad}
		if _, err := LoadFromMap(m); err == nil {
			t.Errorf("EMBER_POLL_MIN_INTERVAL=%q should be rejected", bad)
		}
	}
	// Boundary values accepted.
	for _, ok := range []string{"5m", "24h"} {
		m := map[string]string{"EMBER_SESSION_KEY": strings.Repeat("k", 32), "EMBER_POLL_MIN_INTERVAL": ok}
		if _, err := LoadFromMap(m); err != nil {
			t.Errorf("EMBER_POLL_MIN_INTERVAL=%q should be accepted, got %v", ok, err)
		}
	}
}

func TestLoad_TestModeWaivesSessionKey(t *testing.T) {
	cfg, err := LoadFromMap(map[string]string{
		"EMBER_TEST_MODE": "1",
	})
	if err != nil {
		t.Fatalf("unexpected error in test mode: %v", err)
	}
	if !cfg.TestMode {
		t.Error("TestMode should be true")
	}
}

func TestLoad_MissingSessionKeyRejected(t *testing.T) {
	_, err := LoadFromMap(map[string]string{})
	if err == nil {
		t.Fatal("expected error when EMBER_SESSION_KEY missing")
	}
	if !strings.Contains(err.Error(), "EMBER_SESSION_KEY") {
		t.Errorf("error should mention session key: %v", err)
	}
}

func TestLoad_ShortSessionKeyRejected(t *testing.T) {
	_, err := LoadFromMap(map[string]string{
		"EMBER_SESSION_KEY": "too-short",
	})
	if err == nil {
		t.Fatal("expected error for short session key")
	}
	if !strings.Contains(err.Error(), "at least 32") {
		t.Errorf("error should mention length: %v", err)
	}
}

func TestLoad_InvalidValuesRejected(t *testing.T) {
	cases := map[string]string{
		"EMBER_FRESH_WINDOW":     "not-a-duration",
		"EMBER_POLL_CONCURRENCY": "abc",
		"EMBER_POLL_TICK":        "weeks",
		"EMBER_LOG_LEVEL":        "shout",
		"EMBER_TEST_MODE":        "maybe",
	}
	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			_, err := LoadFromMap(map[string]string{
				"EMBER_SESSION_KEY": strings.Repeat("a", 32),
				k:                   v,
			})
			if err == nil {
				t.Fatalf("expected error for %s=%q", k, v)
			}
		})
	}
}

func TestLoad_NonPositiveDurationsRejected(t *testing.T) {
	_, err := LoadFromMap(map[string]string{
		"EMBER_SESSION_KEY":  strings.Repeat("a", 32),
		"EMBER_FRESH_WINDOW": "0s",
	})
	if err == nil {
		t.Fatal("expected error for zero FreshWindow")
	}
	_, err = LoadFromMap(map[string]string{
		"EMBER_SESSION_KEY":      strings.Repeat("a", 32),
		"EMBER_POLL_CONCURRENCY": "0",
	})
	if err == nil {
		t.Fatal("expected error for zero PollConcurrency")
	}
}

func TestLoad_LogLevels(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"info":    slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"INFO":    slog.LevelInfo,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			cfg, err := LoadFromMap(map[string]string{
				"EMBER_SESSION_KEY": strings.Repeat("a", 32),
				"EMBER_LOG_LEVEL":   in,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.LogLevel != want {
				t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, want)
			}
		})
	}
}

func TestLoad_TestModeBooleanForms(t *testing.T) {
	truthy := []string{"1", "true", "TRUE", "yes", "on"}
	for _, v := range truthy {
		t.Run("truthy/"+v, func(t *testing.T) {
			cfg, err := LoadFromMap(map[string]string{"EMBER_TEST_MODE": v})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !cfg.TestMode {
				t.Errorf("TestMode should be true for %q", v)
			}
		})
	}
	falsy := []string{"0", "false", "no", "off"}
	for _, v := range falsy {
		t.Run("falsy/"+v, func(t *testing.T) {
			cfg, err := LoadFromMap(map[string]string{
				"EMBER_SESSION_KEY": strings.Repeat("a", 32),
				"EMBER_TEST_MODE":   v,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.TestMode {
				t.Errorf("TestMode should be false for %q", v)
			}
		})
	}
}

func TestLoad_SecureCookies(t *testing.T) {
	// Defaults to true.
	cfg, err := LoadFromMap(map[string]string{"EMBER_SESSION_KEY": strings.Repeat("a", 32)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.SecureCookies {
		t.Error("SecureCookies should default to true")
	}
	// Explicit opt-out.
	cfg, err = LoadFromMap(map[string]string{
		"EMBER_SESSION_KEY":    strings.Repeat("a", 32),
		"EMBER_SECURE_COOKIES": "false",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SecureCookies {
		t.Error("SecureCookies should be false when EMBER_SECURE_COOKIES=false")
	}
}

func TestLoad_TrustedProxies(t *testing.T) {
	// Default: empty (trust nobody).
	cfg, err := LoadFromMap(map[string]string{"EMBER_SESSION_KEY": strings.Repeat("a", 32)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.TrustedProxies) != 0 {
		t.Errorf("TrustedProxies should default empty, got %v", cfg.TrustedProxies)
	}
	// CIDRs + bare IPs (bare IPs normalized to /32 //128), comma+space split.
	cfg, err = LoadFromMap(map[string]string{
		"EMBER_SESSION_KEY":     strings.Repeat("a", 32),
		"EMBER_TRUSTED_PROXIES": "172.16.0.0/12, 10.0.0.1 ::1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"172.16.0.0/12", "10.0.0.1/32", "::1/128"}
	if len(cfg.TrustedProxies) != len(want) {
		t.Fatalf("TrustedProxies = %v, want %v", cfg.TrustedProxies, want)
	}
	for i := range want {
		if cfg.TrustedProxies[i] != want[i] {
			t.Errorf("TrustedProxies[%d] = %q, want %q", i, cfg.TrustedProxies[i], want[i])
		}
	}
	// Invalid entry → error.
	if _, err := LoadFromMap(map[string]string{
		"EMBER_SESSION_KEY":     strings.Repeat("a", 32),
		"EMBER_TRUSTED_PROXIES": "not-an-ip",
	}); err == nil {
		t.Error("expected error for invalid EMBER_TRUSTED_PROXIES")
	}
}
