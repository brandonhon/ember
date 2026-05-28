package digest

import (
	"errors"
	"strings"
	"testing"
)

func TestSanitizeAddress(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr error
	}{
		{"plain", "user@example.com", "user@example.com", nil},
		{"named", "Alice <alice@example.com>", "alice@example.com", nil},
		{"trims whitespace", "  user@example.com\t", "user@example.com", nil},
		{"empty", "", "", errBadAddress},
		{"newline injection", "user@example.com\r\nBcc: attacker@evil.com", "", errBadHeader},
		{"lf only", "user@example.com\nBcc: x", "", errBadHeader},
		{"cr only", "user@example.com\rBcc: x", "", errBadHeader},
		{"not an address", "definitely not an email", "", errBadAddress},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := sanitizeAddress(c.in)
			if c.wantErr != nil {
				if !errors.Is(err, c.wantErr) {
					t.Fatalf("want err %v, got %v", c.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestSanitizeHeader(t *testing.T) {
	if _, err := sanitizeHeader("Ember digest — 5 new articles"); err != nil {
		t.Errorf("clean subject rejected: %v", err)
	}
	if _, err := sanitizeHeader("Evil\r\nBcc: attacker@evil.com"); !errors.Is(err, errBadHeader) {
		t.Errorf("CRLF subject should be rejected, got %v", err)
	}
	if _, err := sanitizeHeader("Evil\nBcc: x"); !errors.Is(err, errBadHeader) {
		t.Errorf("LF in subject should be rejected, got %v", err)
	}
}

func TestSendTestMessage_RejectsInjection(t *testing.T) {
	cfg := SMTPConfig{Host: "smtp.example.com", Port: 587, From: "ember@example.com", StartTLS: true}
	cases := map[string]struct {
		to      string
		appName string
	}{
		"crlf in to":       {"user@example.com\r\nBcc: a@b.com", "Ember"},
		"crlf in app name": {"user@example.com", "Evil\r\nBcc: a@b.com"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			err := SendTestMessage(cfg, c.to, c.appName)
			if err == nil {
				t.Fatal("expected sanitize error, got nil (would have attempted SMTP)")
			}
			// Must reject before any SMTP dial — sanitizer errors only.
			if !errors.Is(err, errBadHeader) && !errors.Is(err, errBadAddress) {
				t.Errorf("expected sanitizer error, got %v", err)
			}
			if strings.Contains(err.Error(), "dial") || strings.Contains(err.Error(), "connection") {
				t.Errorf("sanitize should reject before network: %v", err)
			}
		})
	}
}

func TestBuildMIME_HeadersAreClean(t *testing.T) {
	// Smoke check: when fed sanitized inputs, the MIME output has no stray
	// CRLF in the headers (CRLFs are only allowed as line terminators).
	msg := buildMIME("ember@example.com", "user@example.com", "Subject line", "plain", "<p>html</p>")
	headerBlock, _, ok := strings.Cut(string(msg), "\r\n\r\n")
	if !ok {
		t.Fatalf("MIME missing header terminator:\n%s", msg)
	}
	for line := range strings.SplitSeq(headerBlock, "\r\n") {
		// No bare LF or extra CR in any individual header line.
		if strings.ContainsAny(line, "\r\n") {
			t.Errorf("header line contains bare CR/LF: %q", line)
		}
	}
}
