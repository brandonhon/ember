package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SecurityHeaders returns middleware that sets common hardening headers. When
// the app sits behind a TLS-terminating proxy these complement the proxy's
// own; exposed directly they are the only source. `trusted` is the set of
// proxy CIDRs whose X-Forwarded-Proto is believed when deciding whether the
// edge connection is HTTPS (for the HSTS header).
func SecurityHeaders(trusted []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			// HSTS only over HTTPS: browsers ignore (and RFC 6797 says to
			// ignore) HSTS received over plain HTTP, and emitting it there is a
			// misleading no-op. Detect HTTPS from the connection or from a
			// trusted proxy's X-Forwarded-Proto. 2 years + includeSubDomains.
			if httpsDetected(r, trusted) {
				h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			// Disable browser features we never use. Defense in depth against XSS
			// chains that try to exfil via webcam, geolocation, etc.
			h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), payment=(), usb=()")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Cross-Origin-Resource-Policy", "same-origin")
			// CSP — locked down to the same origin except for the Google Fonts
			// stylesheets and webfonts. The mockup's typography is critical to
			// the design language. img-src allows any https: origin because
			// feeds embed third-party article images; style-src allows
			// 'unsafe-inline' for the SPA's scoped styles. Both are accepted
			// trade-offs for a self-hosted reader (documented in security docs).
			h.Set("Content-Security-Policy",
				"default-src 'self'; "+
					"img-src 'self' data: https:; "+
					"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
					"font-src 'self' data: https://fonts.gstatic.com; "+
					"connect-src 'self'; "+
					"object-src 'none'; "+
					"base-uri 'self'; "+
					"form-action 'self'; "+
					"frame-ancestors 'none'")
			next.ServeHTTP(w, r)
		})
	}
}

// httpsDetected reports whether the edge connection to the user is HTTPS. True
// when the request reached us over TLS directly, or when a trusted proxy
// forwarded X-Forwarded-Proto: https. Untrusted X-Forwarded-Proto is ignored
// so a direct attacker can't fake HTTPS to coax out an HSTS header.
func httpsDetected(r *http.Request, trusted []*net.IPNet) bool {
	if r.TLS != nil {
		return true
	}
	if peerTrusted(r, trusted) {
		if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			return true
		}
	}
	return false
}

// RateLimiter is a tiny in-memory leaky-bucket keyed by remote IP. Suitable
// for a single-instance self-hosted deployment; not for fleets. Goroutine-safe.
type RateLimiter struct {
	// MaxBurst tokens may be consumed instantaneously; tokens regenerate
	// at MaxBurst / WindowPeriod.
	MaxBurst     int
	WindowPeriod time.Duration
	// trusted is the set of proxy CIDRs whose X-Real-IP is honored when keying
	// the bucket. Empty = key on the connection peer (the app is the edge).
	trusted []*net.IPNet

	mu      sync.Mutex
	buckets map[string]*bucket
	last    time.Time
}

type bucket struct {
	tokens  float64
	updated time.Time
}

// NewRateLimiter returns a limiter that allows `burst` requests instantly and
// then refills at `burst/window` per second. `trusted` is the set of proxy
// CIDRs whose X-Real-IP header is honored for bucket keying; pass nil to key
// strictly on the connection peer.
func NewRateLimiter(burst int, window time.Duration, trusted []*net.IPNet) *RateLimiter {
	return &RateLimiter{
		MaxBurst:     burst,
		WindowPeriod: window,
		trusted:      trusted,
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
		key := remoteIP(r, rl.trusted)
		if !rl.Allow(key) {
			writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ParseTrustedProxies converts CIDR strings (already validated by config) into
// *net.IPNet. Invalid entries are skipped defensively. An empty/nil input
// yields nil — meaning "trust no proxy": ember is the edge and reads the real
// client from the connection, ignoring X-Real-IP / X-Forwarded-Proto.
func ParseTrustedProxies(cidrs []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		if _, n, err := net.ParseCIDR(c); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// peerTrusted reports whether the immediate connection peer (RemoteAddr) is
// inside one of the trusted proxy CIDRs.
func peerTrusted(r *http.Request, trusted []*net.IPNet) bool {
	if len(trusted) == 0 {
		return false
	}
	ip := net.ParseIP(hostOnly(r.RemoteAddr))
	if ip == nil {
		return false
	}
	for _, n := range trusted {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// hostOnly strips the :port (and IPv6 brackets) from a host:port string.
func hostOnly(hostPort string) string {
	h := hostPort
	if i := strings.LastIndexByte(h, ':'); i > 0 {
		h = h[:i]
	}
	return strings.TrimPrefix(strings.TrimSuffix(h, "]"), "[")
}

// remoteIP returns the rate-limit key for the request: the forwarded X-Real-IP
// when the connection peer is a trusted proxy, otherwise the connection peer
// itself. Untrusted X-Real-IP is ignored so a direct client can't forge it to
// bypass the limiter or poison another IP's bucket.
func remoteIP(r *http.Request, trusted []*net.IPNet) string {
	if peerTrusted(r, trusted) {
		if v := r.Header.Get("X-Real-IP"); v != "" {
			return v
		}
	}
	return hostOnly(r.RemoteAddr)
}

// CSRFCookieName is the cookie the API sets carrying the CSRF token.
const CSRFCookieName = "ember_csrf"

// CSRFHeaderName is the header the SPA echoes the cookie value on. Double-
// submit pattern — both must match.
const CSRFHeaderName = "X-Ember-CSRF"

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
		// Login is the bootstrap path — no cookie yet. Skip. Use exact match
		// so a routing mishap that mounts /api under a sub-path can't expose
		// other endpoints to the bypass.
		if r.URL.Path == "/api/auth/login" {
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
		// Constant-time compare: the CSRF token is a secret, so avoid leaking
		// match progress via timing on the != comparison.
		if header == "" || subtle.ConstantTimeCompare([]byte(header), []byte(cookie.Value)) != 1 {
			writeError(w, http.StatusForbidden, "csrf_mismatch", "csrf token invalid")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func mustRandHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// A failing CSRF token must fail closed, not open: a static fallback
		// would hand every session the same forgeable token. crypto/rand
		// failing is unrecoverable, so panic — middleware.Recoverer catches it
		// and returns 500, and no broken token is ever issued.
		panic("api: crypto/rand.Read failed generating CSRF token: " + err.Error())
	}
	return hex.EncodeToString(b)
}
