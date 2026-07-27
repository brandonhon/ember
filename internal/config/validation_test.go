package config

import (
	"strings"
	"testing"
)

// withKey builds a minimal valid environment plus one override.
func withKey(k, v string) map[string]string {
	return map[string]string{
		"EMBER_SESSION_KEY": strings.Repeat("a", 32),
		k:                   v,
	}
}

// Every validated variable must reject a bad value, and the resulting message
// must name the variable that caused it. Operators read this at boot with no
// other context, so "invalid duration" pointing at the wrong knob is close to
// useless — and it is exactly the slip a table-driven loader invites.
func TestLoad_InvalidValueNamesItsVariable(t *testing.T) {
	bad := map[string]string{
		"EMBER_OLLAMA_URL":           "://not-a-url",
		"EMBER_FRESH_WINDOW":         "not-a-duration",
		"EMBER_SESSION_TTL":          "not-a-duration",
		"EMBER_POLL_CONCURRENCY":     "abc",
		"EMBER_POLL_TICK":            "weeks",
		"EMBER_POLL_MIN_INTERVAL":    "nope",
		"EMBER_LOG_LEVEL":            "shout",
		"EMBER_TEST_MODE":            "maybe",
		"EMBER_DISABLE_SUMMARIES":    "perhaps",
		"EMBER_DISABLE_IMAGES":       "perhaps",
		"EMBER_DISABLE_UPDATE_CHECK": "perhaps",
		"EMBER_SECURE_COOKIES":       "perhaps",
		"EMBER_ALLOW_PRIVATE_URLS":   "perhaps",
		"EMBER_HSTS_PRELOAD":         "perhaps",
		"EMBER_SMTP_STARTTLS":        "perhaps",
		"EMBER_SMTP_PORT":            "70000",
		"EMBER_EMAIL_MAX_BYTES":      "0",
		"EMBER_TRUSTED_PROXIES":      "not-an-ip",
	}
	for k, v := range bad {
		t.Run(k, func(t *testing.T) {
			_, err := LoadFromMap(withKey(k, v))
			if err == nil {
				t.Fatalf("%s=%q was accepted, want an error", k, v)
			}
			if !strings.Contains(err.Error(), k) {
				t.Errorf("%s=%q -> %q, want the message to name %s", k, v, err.Error(), k)
			}
		})
	}
}

// Out-of-range values must be rejected just like unparseable ones.
func TestLoad_RangeViolationsRejected(t *testing.T) {
	for _, tc := range []struct{ key, val, why string }{
		{"EMBER_FRESH_WINDOW", "0s", "zero duration"},
		{"EMBER_SESSION_TTL", "-1h", "negative duration"},
		{"EMBER_POLL_TICK", "0s", "zero duration"},
		{"EMBER_POLL_CONCURRENCY", "0", "below minimum of 1"},
		{"EMBER_POLL_MIN_INTERVAL", "1m", "below the 5m floor"},
		{"EMBER_POLL_MIN_INTERVAL", "48h", "above the 24h ceiling"},
		{"EMBER_SMTP_PORT", "0", "below port range"},
		{"EMBER_SMTP_PORT", "65536", "above port range"},
		{"EMBER_EMAIL_MAX_BYTES", "-1", "negative size"},
	} {
		t.Run(tc.key+"="+tc.val, func(t *testing.T) {
			if _, err := LoadFromMap(withKey(tc.key, tc.val)); err == nil {
				t.Errorf("%s=%q accepted, want rejection (%s)", tc.key, tc.val, tc.why)
			}
		})
	}
}

// Values at the edge of each range must be accepted — an off-by-one in a
// shared bounds check would otherwise reject a legitimate setting.
func TestLoad_RangeBoundariesAccepted(t *testing.T) {
	for _, tc := range []struct{ key, val string }{
		{"EMBER_POLL_MIN_INTERVAL", "5m"},
		{"EMBER_POLL_MIN_INTERVAL", "24h"},
		{"EMBER_POLL_CONCURRENCY", "1"},
		{"EMBER_SMTP_PORT", "1"},
		{"EMBER_SMTP_PORT", "65535"},
		{"EMBER_EMAIL_MAX_BYTES", "1"},
	} {
		t.Run(tc.key+"="+tc.val, func(t *testing.T) {
			if _, err := LoadFromMap(withKey(tc.key, tc.val)); err != nil {
				t.Errorf("%s=%q rejected: %v", tc.key, tc.val, err)
			}
		})
	}
}

// An unset or empty variable must leave the default untouched rather than
// clobbering it with a zero value.
func TestLoad_EmptyValueLeavesDefault(t *testing.T) {
	def := Defaults()
	env := map[string]string{"EMBER_SESSION_KEY": strings.Repeat("a", 32)}
	for _, k := range []string{
		"EMBER_ADDR", "EMBER_DB_PATH", "EMBER_ADMIN_USER", "EMBER_OLLAMA_URL",
		"EMBER_OLLAMA_MODEL", "EMBER_FRESH_WINDOW", "EMBER_SESSION_TTL",
		"EMBER_POLL_CONCURRENCY", "EMBER_POLL_TICK", "EMBER_POLL_MIN_INTERVAL",
		"EMBER_LOG_LEVEL", "EMBER_SMTP_PORT", "EMBER_SMTP_STARTTLS",
		"EMBER_SECURE_COOKIES",
	} {
		env[k] = ""
	}
	got, err := LoadFromMap(env)
	if err != nil {
		t.Fatalf("empty values rejected: %v", err)
	}
	if got.Addr != def.Addr || got.DBPath != def.DBPath || got.AdminUser != def.AdminUser {
		t.Errorf("string defaults clobbered: %+v", got)
	}
	if got.OllamaURL != def.OllamaURL || got.OllamaModel != def.OllamaModel {
		t.Errorf("ollama defaults clobbered: %q %q", got.OllamaURL, got.OllamaModel)
	}
	if got.FreshWindow != def.FreshWindow || got.SessionTTL != def.SessionTTL ||
		got.PollTick != def.PollTick || got.PollMinInterval != def.PollMinInterval {
		t.Errorf("duration defaults clobbered: %+v", got)
	}
	if got.PollConcurrency != def.PollConcurrency || got.SMTPPort != def.SMTPPort {
		t.Errorf("numeric defaults clobbered: %d %d", got.PollConcurrency, got.SMTPPort)
	}
	if got.LogLevel != def.LogLevel {
		t.Errorf("LogLevel = %v, want %v", got.LogLevel, def.LogLevel)
	}
	if !got.SMTPStartTLS || !got.SecureCookies {
		t.Errorf("bool defaults clobbered: starttls=%v secure=%v", got.SMTPStartTLS, got.SecureCookies)
	}
}

// All errors from one load must be reported together, not just the first —
// an operator fixing a bad compose file should see every problem at once.
func TestLoad_ReportsAllErrorsAtOnce(t *testing.T) {
	_, err := LoadFromMap(map[string]string{
		"EMBER_SESSION_KEY":      strings.Repeat("a", 32),
		"EMBER_FRESH_WINDOW":     "nope",
		"EMBER_POLL_CONCURRENCY": "abc",
		"EMBER_LOG_LEVEL":        "shout",
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"EMBER_FRESH_WINDOW", "EMBER_POLL_CONCURRENCY", "EMBER_LOG_LEVEL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("combined error %q missing %s", err.Error(), want)
		}
	}
}
