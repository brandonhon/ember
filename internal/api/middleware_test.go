package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSecurityHeaders(t *testing.T) {
	h := SecurityHeaders(nil, false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(w, r)
	want := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "strict-origin-when-cross-origin",
		"Content-Security-Policy": "default-src 'self'",
	}
	for k, prefix := range want {
		got := w.Header().Get(k)
		if !strings.Contains(got, prefix) {
			t.Errorf("%s = %q, want substring %q", k, got, prefix)
		}
	}
	// Plain HTTP, no trusted proxy → no HSTS (RFC 6797: ignored over non-HTTPS).
	if got := w.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS should be absent over plain HTTP, got %q", got)
	}
}

func TestSecurityHeaders_HSTSGating(t *testing.T) {
	trusted := ParseTrustedProxies([]string{"10.0.0.0/8"})
	h := SecurityHeaders(trusted, false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Trusted proxy reports HTTPS → HSTS set.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.1.2.3:5000"
	r.Header.Set("X-Forwarded-Proto", "https")
	h.ServeHTTP(w, r)
	if w.Header().Get("Strict-Transport-Security") == "" {
		t.Error("HSTS should be set when a trusted proxy reports https")
	}

	// Untrusted peer with the same header → ignored, no HSTS.
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.RemoteAddr = "8.8.8.8:5000"
	r2.Header.Set("X-Forwarded-Proto", "https")
	h.ServeHTTP(w2, r2)
	if got := w2.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("untrusted X-Forwarded-Proto must not trigger HSTS, got %q", got)
	}
}

func TestRemoteIP_TrustBoundary(t *testing.T) {
	trusted := ParseTrustedProxies([]string{"10.0.0.0/8"})

	// Trusted peer → X-Real-IP honored.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.5:1234"
	r.Header.Set("X-Real-IP", "1.2.3.4")
	if got := remoteIP(r, trusted); got != "1.2.3.4" {
		t.Errorf("trusted peer: got %q, want forwarded 1.2.3.4", got)
	}

	// Untrusted peer → X-Real-IP ignored, connection peer used.
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.RemoteAddr = "203.0.113.9:1234"
	r2.Header.Set("X-Real-IP", "1.2.3.4")
	if got := remoteIP(r2, trusted); got != "203.0.113.9" {
		t.Errorf("untrusted peer: got %q, want connection peer 203.0.113.9", got)
	}

	// No trusted set → never honor the header.
	r3 := httptest.NewRequest(http.MethodGet, "/", nil)
	r3.RemoteAddr = "10.0.0.5:1234"
	r3.Header.Set("X-Real-IP", "1.2.3.4")
	if got := remoteIP(r3, nil); got != "10.0.0.5" {
		t.Errorf("no trusted set: got %q, want connection peer 10.0.0.5", got)
	}
}

func TestRateLimiter_AllowAndDeny(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute, nil)
	for range 3 {
		if !rl.Allow("ip-1") {
			t.Errorf("should allow within burst")
		}
	}
	if rl.Allow("ip-1") {
		t.Errorf("should deny after burst exhausted")
	}
	// Different key has its own bucket.
	if !rl.Allow("ip-2") {
		t.Errorf("ip-2 should be independent")
	}
}

func TestRateLimiter_Middleware(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute, nil)
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := rl.LimitMiddleware(inner)

	for i := range 3 {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = "127.0.0.1:1234"
		mw.ServeHTTP(w, r)
		if i < 2 {
			if w.Code != http.StatusOK {
				t.Errorf("burst %d: %d", i, w.Code)
			}
		} else {
			if w.Code != http.StatusTooManyRequests {
				t.Errorf("burst %d: expected 429, got %d", i, w.Code)
			}
		}
	}
}

func TestCSRFVerify(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := CSRFVerify(inner)

	// GET passes without token.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/feeds", nil)
	mw.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("GET = %d", w.Code)
	}

	// Login bypasses CSRF.
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	mw.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("login = %d", w.Code)
	}

	// POST with NO session cookie → passes (RequireAuth will 401 it).
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/api/articles/star", nil)
	mw.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("no session, no csrf = %d (should pass to RequireAuth)", w.Code)
	}

	// POST WITH session but no CSRF → 403.
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/api/articles/star", nil)
	r.AddCookie(&http.Cookie{Name: "ember_session", Value: "dummy"})
	mw.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("session w/o csrf = %d", w.Code)
	}

	// POST with session + mismatched header → 403.
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/api/articles/star", nil)
	r.AddCookie(&http.Cookie{Name: "ember_session", Value: "dummy"})
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "abc"})
	r.Header.Set(CSRFHeaderName, "xyz")
	mw.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("mismatch = %d", w.Code)
	}

	// POST with session + matching cookie+header → 200.
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/api/articles/star", nil)
	r.AddCookie(&http.Cookie{Name: "ember_session", Value: "dummy"})
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "same-token"})
	r.Header.Set(CSRFHeaderName, "same-token")
	mw.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("match = %d", w.Code)
	}
}

func TestCSRFIssue_SetsCookieOnce(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := CSRFIssue(true)(inner)

	// First request — no cookie → response sets one.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	mw.ServeHTTP(w, r)
	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == CSRFCookieName && len(c.Value) > 0 {
			found = true
		}
	}
	if !found {
		t.Error("CSRFIssue did not set the cookie")
	}

	// Second request — already has cookie → no Set-Cookie issued.
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "preexisting"})
	mw.ServeHTTP(w, r)
	for _, c := range w.Result().Cookies() {
		if c.Name == CSRFCookieName {
			t.Errorf("should not re-issue cookie when present")
		}
	}
}

func TestHealthEndpoints(t *testing.T) {
	h := newHarness(t)
	resp, err := h.srv.Client().Get(h.srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz = %d", resp.StatusCode)
	}
	resp, err = h.srv.Client().Get(h.srv.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/readyz = %d", resp.StatusCode)
	}
}

// TestMethodNotAllowedHasSecurityHeaders verifies the 405 path (a known route
// hit with the wrong method) goes through SecurityHeaders rather than chi's
// bare default handler.
func TestMethodNotAllowedHasSecurityHeaders(t *testing.T) {
	h := newHarness(t)
	// /api/auth/login is POST-only; a GET should 405.
	resp, err := h.srv.Client().Get(h.srv.URL + "/api/auth/login")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/auth/login = %d, want 405", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("405 missing security headers: X-Content-Type-Options = %q", got)
	}
	if got := resp.Header.Get("Content-Security-Policy"); got == "" {
		t.Error("405 missing Content-Security-Policy header")
	}
}
