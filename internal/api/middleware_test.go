package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSecurityHeaders(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
}

func TestRateLimiter_AllowAndDeny(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)
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
	rl := NewRateLimiter(2, time.Minute)
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
	mw := CSRFIssue(inner)

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
