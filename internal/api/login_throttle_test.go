package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/auth"
)

// postLogin sends one login attempt on a throwaway client and returns the
// status plus the Retry-After header.
func postLogin(t *testing.T, h *harness, username, password string) (int, string) {
	t.Helper()
	jar, _ := newJar()
	cl := h.newClient(jar)
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	resp, err := cl.Post(h.srv.URL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, resp.Header.Get("Retry-After")
}

func TestLogin_ThrottleReturns429WithRetryAfter(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "correct-horse", false)

	// Burn the free allowance. Every one of these is a plain 401.
	for i := 0; i < auth.LoginFreeAttempts; i++ {
		code, _ := postLogin(t, h, "alice", "wrong")
		if code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d, want 401", i+1, code)
		}
	}

	code, retryAfter := postLogin(t, h, "alice", "wrong")
	if code != http.StatusTooManyRequests {
		t.Fatalf("throttled attempt: status %d, want 429", code)
	}
	if retryAfter == "" {
		t.Fatal("throttled response has no Retry-After header")
	}
	secs, err := strconv.Atoi(retryAfter)
	if err != nil {
		t.Fatalf("Retry-After %q is not an integer number of seconds: %v", retryAfter, err)
	}
	// Must be a usable, non-zero wait — a 0 would invite an immediate retry
	// that is still inside the window.
	if secs < 1 || time.Duration(secs)*time.Second > auth.LoginBackoffCap {
		t.Errorf("Retry-After = %ds, want between 1 and %v", secs, auth.LoginBackoffCap)
	}
}

// The throttle keys on the submitted username, so it must fire identically for
// an account that doesn't exist. A difference here would let an attacker
// distinguish real usernames just by counting attempts until the 429.
func TestLogin_ThrottleDoesNotRevealAccountExistence(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "correct-horse", false)

	attemptsUntil429 := func(username string) int {
		for i := 1; i <= auth.LoginFreeAttempts+3; i++ {
			code, _ := postLogin(t, h, username, "wrong")
			if code == http.StatusTooManyRequests {
				return i
			}
		}
		return -1
	}
	real := attemptsUntil429("alice")
	fake := attemptsUntil429("nobody-here")
	if real < 1 {
		t.Fatal("real account never throttled")
	}
	if real != fake {
		t.Errorf("throttle fired after %d attempts for a real account but %d for a nonexistent one", real, fake)
	}
}

// A throttled response must not carry a session cookie.
func TestLogin_ThrottledResponseIssuesNoSession(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "correct-horse", false)
	for i := 0; i < auth.LoginFreeAttempts; i++ {
		postLogin(t, h, "alice", "wrong")
	}

	jar, _ := newJar()
	cl := h.newClient(jar)
	// Correct password, but inside the backoff window.
	body, _ := json.Marshal(map[string]string{"username": "alice", "password": "correct-horse"})
	resp, err := cl.Post(h.srv.URL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status %d, want 429 even with the correct password", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			t.Fatal("throttled login issued a session cookie")
		}
	}
}
