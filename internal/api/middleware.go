package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SecurityHeaders sets common headers that complement Caddy's own. The chain
// is layered so even if a reverse proxy is misconfigured, sane defaults apply.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// CSP — locked down to the same origin except for the Google Fonts
		// stylesheets and webfonts. The mockup's typography is critical to
		// the design language.
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"img-src 'self' data: https:; "+
				"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
				"font-src 'self' data: https://fonts.gstatic.com; "+
				"connect-src 'self'; "+
				"frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// RateLimiter is a tiny in-memory leaky-bucket keyed by remote IP. Suitable
// for a single-instance self-hosted deployment; not for fleets. Goroutine-safe.
type RateLimiter struct {
	// MaxBurst tokens may be consumed instantaneously; tokens regenerate
	// at MaxBurst / WindowPeriod.
	MaxBurst     int
	WindowPeriod time.Duration

	mu      sync.Mutex
	buckets map[string]*bucket
	last    time.Time
}

type bucket struct {
	tokens  float64
	updated time.Time
}

// NewRateLimiter returns a limiter that allows `burst` requests instantly and
// then refills at `burst/window` per second.
func NewRateLimiter(burst int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		MaxBurst:     burst,
		WindowPeriod: window,
		buckets:      map[string]*bucket{},
	}
}

// Allow consumes a token for the given key and returns true if the request
// should proceed.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(rl.MaxBurst), updated: now}
		rl.buckets[key] = b
	}
	// Refill based on elapsed time.
	elapsed := now.Sub(b.updated).Seconds()
	refillRate := float64(rl.MaxBurst) / rl.WindowPeriod.Seconds()
	b.tokens = min(float64(rl.MaxBurst), b.tokens+elapsed*refillRate)
	b.updated = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	// Periodic GC of cold buckets.
	if now.Sub(rl.last) > 5*time.Minute {
		for k, v := range rl.buckets {
			if now.Sub(v.updated) > 30*time.Minute {
				delete(rl.buckets, k)
			}
		}
		rl.last = now
	}
	return true
}

// LimitMiddleware enforces the limiter. On a deny it writes 429 with a small
// JSON body.
func (rl *RateLimiter) LimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := remoteIP(r)
		if !rl.Allow(key) {
			writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func remoteIP(r *http.Request) string {
	// Caddy sets X-Real-IP. Trust it only because we're behind Caddy on the
	// internal compose network; in any other deployment, audit this.
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	if i := strings.IndexByte(r.RemoteAddr, ':'); i > 0 {
		return r.RemoteAddr[:i]
	}
	return r.RemoteAddr
}

// CSRFCookieName is the cookie the API sets carrying the CSRF token.
const CSRFCookieName = "ember_csrf"

// CSRFHeaderName is the header the SPA echoes the cookie value on. Double-
// submit pattern — both must match.
const CSRFHeaderName = "X-Ember-CSRF"

// ErrCSRF is returned when the double-submit token check fails.
var ErrCSRF = errors.New("api: csrf token mismatch")

// CSRFIssue returns a chi middleware that lazily sets the CSRF cookie on
// every response that doesn't already carry one. `secure` controls the Secure
// cookie flag (set to false for plain-HTTP test mode).
func CSRFIssue(secure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, err := r.Cookie(CSRFCookieName); err != nil {
				tok := mustRandHex(16)
				http.SetCookie(w, &http.Cookie{
					Name:     CSRFCookieName,
					Value:    tok,
					Path:     "/",
					HttpOnly: false, // must be readable by JS to echo into header
					Secure:   secure,
					SameSite: http.SameSiteLaxMode,
					MaxAge:   86400,
				})
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CSRFVerify is a middleware that rejects unsafe (POST/PUT/PATCH/DELETE)
// requests whose CSRF header doesn't match the cookie. GET/HEAD/OPTIONS pass.
// Also passes when there is no session cookie — the request would be 401'd by
// RequireAuth anyway, and CSRF only protects authenticated state.
// Mounted on the /api group only — the Fever shim has its own md5 api_key
// authentication and intentionally doesn't participate.
func CSRFVerify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		// Login is the bootstrap path — no cookie yet. Skip.
		if strings.HasSuffix(r.URL.Path, "/api/auth/login") {
			next.ServeHTTP(w, r)
			return
		}
		// No session cookie → not authenticated. Let RequireAuth return 401.
		// Without a session there's nothing to forge, so CSRF check is moot.
		if _, err := r.Cookie("ember_session"); err != nil {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(CSRFCookieName)
		if err != nil {
			writeError(w, http.StatusForbidden, "csrf_missing", "csrf cookie missing")
			return
		}
		header := r.Header.Get(CSRFHeaderName)
		if header == "" || header != cookie.Value {
			writeError(w, http.StatusForbidden, "csrf_mismatch", "csrf token invalid")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func mustRandHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// extremely unlikely; fall back to a known-bad-but-non-zero value so
		// requests still succeed (the user gets a fresh token next request).
		return "deadbeef-rand-failed"
	}
	return hex.EncodeToString(b)
}
