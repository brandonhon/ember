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

// frozenClock pins the store and auth clocks to the same fixed instant.
//
// The throttle compares now against the last-failure time, which the store
// keeps in whole Unix seconds. The first backoff step is only LoginBackoffBase
// (1s), so with a live clock the window can lapse between the fifth failure
// and the sixth attempt and the expected 429 arrives as a plain 401 — a real
// CI flake, since five argon2 hashes under -race on a loaded runner take
// longer than that. Second-granularity truncation makes it worse: a failure
// stamped at X.9s is read back as X, donating most of the window away.
//
// Both clocks must move together. Freezing only the auth side would leave it
// comparing a fixed instant against wall-clock failure stamps, and the
// resulting negative elapsed inflates Retry-After past LoginBackoffCap.
// The instant is an exact second boundary so no sub-second remainder is left
// to erode the window.
func frozenClock(d *Dependencies) {
	now := time.Unix(1700000000, 0)
	d.Store.Now = func() time.Time { return now }
	d.Auth.Now = func() time.Time { return now }
}

func TestLogin_ThrottleReturns429WithRetryAfter(t *testing.T) {
	h := newHarnessWith(t, frozenClock)
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
	h := newHarnessWith(t, frozenClock)
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
	h := newHarnessWith(t, frozenClock)
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

// reauthAttempt sends one wrong-current-password request to a re-auth endpoint
// on an already signed-in client and returns the status plus Retry-After.
func reauthAttempt(t *testing.T, cl *http.Client, method, url string, body map[string]string) (int, string) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(method, url, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	echoCSRF(cl, url, req)
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, resp.Header.Get("Retry-After")
}

// The self-service password and email changes verify the current password.
// Both must sit behind the same per-username backoff as login: otherwise a
// stolen session is an unthrottled oracle for guessing the account password
// (and the guess that lands lets the attacker change it and lock the owner
// out). Misses on either endpoint count toward the shared backoff, the
// backoff refuses even the correct password, and the 429 carries Retry-After
// exactly as login's does.
func TestReauth_SharesLoginThrottle(t *testing.T) {
	h := newHarnessWith(t, frozenClock)
	h.seedUser(t, "alice", "correct-horse", false)
	cl := h.login(t, "alice", "correct-horse")

	pwURL := h.srv.URL + "/api/me/password"
	emailURL := h.srv.URL + "/api/me/email"
	wrongPW := map[string]string{"old_password": "wrong", "new_password": "attacker-chosen"}
	wrongEmail := map[string]string{"email": "x@example.com", "current_password": "wrong"}

	// Spend the free allowance across BOTH endpoints: they share one counter.
	for i := 0; i < auth.LoginFreeAttempts; i++ {
		var code int
		if i%2 == 0 {
			code, _ = reauthAttempt(t, cl, http.MethodPost, pwURL, wrongPW)
		} else {
			code, _ = reauthAttempt(t, cl, http.MethodPatch, emailURL, wrongEmail)
		}
		if code != http.StatusUnauthorized {
			t.Fatalf("re-auth miss %d: status %d, want 401", i+1, code)
		}
	}

	// Next attempt on either endpoint is throttled, even with the right
	// password — the check runs before the hash, as on login.
	rightPW := map[string]string{"old_password": "correct-horse", "new_password": "attacker-chosen"}
	code, retry := reauthAttempt(t, cl, http.MethodPost, pwURL, rightPW)
	if code != http.StatusTooManyRequests {
		t.Fatalf("password change after %d misses: status %d, want 429", auth.LoginFreeAttempts, code)
	}
	if n, err := strconv.Atoi(retry); err != nil || n < 1 || n > int(auth.LoginBackoffCap.Seconds()) {
		t.Fatalf("Retry-After = %q, want 1..%d", retry, int(auth.LoginBackoffCap.Seconds()))
	}
	rightEmail := map[string]string{"email": "x@example.com", "current_password": "correct-horse"}
	if code, _ := reauthAttempt(t, cl, http.MethodPatch, emailURL, rightEmail); code != http.StatusTooManyRequests {
		t.Fatalf("email change during backoff: status %d, want 429", code)
	}

	// The backoff is the login backoff: a fresh login for the same username is
	// refused too, so re-auth misses can't be laundered through one door and
	// spent at the other.
	if code, _ := postLogin(t, h, "alice", "correct-horse"); code != http.StatusTooManyRequests {
		t.Fatalf("login during re-auth backoff: status %d, want 429", code)
	}

	// And the password was never changed by the throttled attempt.
	if code, _ := postLogin(t, h, "alice", "attacker-chosen"); code != http.StatusTooManyRequests {
		t.Fatalf("login with the attacker's password: status %d, want 429 (still throttled, never set)", code)
	}
}
