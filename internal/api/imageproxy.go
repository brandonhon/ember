package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/brandonhon/ember/internal/urlcheck"
)

// imageProxy serves remote article images through ember's own origin. Content
// blockers (uBlock, Privacy Badger, …) key on publisher CDN domains, so a lead
// image fetched directly from e.g. a57.foxnews.com gets stripped before it
// renders; routed through this same-origin endpoint it loads normally.
//
// Source URLs are HMAC-signed by the server when it rewrites article responses
// (see rewrite), so this endpoint is a capability, not an open relay: it only
// fetches URLs ember itself authorized. The outbound client is SSRF-guarded
// and size/time-bounded so a hostile origin can't pivot or exhaust us.
type imageProxy struct {
	key          []byte
	client       *http.Client
	allowPrivate bool
}

const (
	// imageProxyMaxBytes caps a single proxied image. Article lead images are
	// well under this; the cap bounds memory/bandwidth for a hostile origin.
	imageProxyMaxBytes = 10 << 20 // 10 MiB
	// imageProxyTimeout bounds the whole outbound fetch.
	imageProxyTimeout = 15 * time.Second
	// imageProxyUA — a few publisher CDNs 403 requests without a browser-ish UA.
	imageProxyUA = "Mozilla/5.0 (compatible; ember/1.0; +https://github.com/brandonhon/ember)"
	// imageProxyCacheControl lets browsers and any fronting cache hold the
	// image for a day; stream-through keeps origin hits down without a server cache.
	imageProxyCacheControl = "public, max-age=86400"
)

// newImageProxy derives a dedicated HMAC key from the session key (domain-
// separated so a proxy signature can never be confused with another use of the
// session key) and builds the SSRF-guarded, bounded HTTP client.
func newImageProxy(sessionKey string, allowPrivate bool) *imageProxy {
	mac := hmac.New(sha256.New, []byte(sessionKey))
	mac.Write([]byte("ember-image-proxy-v1"))
	return &imageProxy{
		key:          mac.Sum(nil),
		allowPrivate: allowPrivate,
		client: &http.Client{
			Timeout:   imageProxyTimeout,
			Transport: urlcheck.GuardedTransport(allowPrivate),
		},
	}
}

// sign returns the base64url HMAC of a source URL.
func (p *imageProxy) sign(src string) string {
	mac := hmac.New(sha256.New, p.key)
	mac.Write([]byte(src))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// rewrite turns a raw image URL into a same-origin proxied path. Empty in =>
// empty out (preserving the json omitempty + frontend `{#if image_url}`
// behavior). Non-http(s) values (e.g. data: URIs) pass through unchanged —
// they're already same-origin-safe and there's nothing to proxy.
func (p *imageProxy) rewrite(src string) string {
	if src == "" {
		return ""
	}
	if !strings.HasPrefix(src, "http://") && !strings.HasPrefix(src, "https://") {
		return src
	}
	q := url.Values{}
	q.Set("u", src)
	q.Set("s", p.sign(src))
	return "/api/img?" + q.Encode()
}

// handle fetches a signed image URL and streams it back same-origin.
func (p *imageProxy) handle(w http.ResponseWriter, r *http.Request) {
	src := r.URL.Query().Get("u")
	sig := r.URL.Query().Get("s")
	if src == "" || sig == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "missing image params")
		return
	}
	// Constant-time verify the signature before any network I/O: only URLs the
	// server signed are honored, which is what keeps this from being an open
	// proxy / SSRF relay.
	if !hmac.Equal([]byte(sig), []byte(p.sign(src))) {
		writeError(w, http.StatusForbidden, "forbidden", "bad image signature")
		return
	}
	// Pre-flight SSRF check (the transport DialContext also guards the actual
	// connect, covering DNS-rebind between this check and the dial).
	if err := urlcheck.Check(r.Context(), src, p.allowPrivate); err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "image url not allowed")
		return
	}

	// src is not attacker-controlled at this point: the HMAC signature check
	// above only admits URLs the server itself signed during article rewrite,
	// and urlcheck.Check + GuardedTransport guard the actual network reach.
	// Static taint analyzers can't see through the HMAC equality, so we tell
	// gosec/CodeQL explicitly.
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, src, nil) //nolint:gosec // SSRF guarded by HMAC verify + urlcheck.Check + GuardedTransport
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "bad image url")
		return
	}
	req.Header.Set("User-Agent", imageProxyUA)
	req.Header.Set("Accept", "image/*")

	resp, err := p.client.Do(req) //nolint:gosec // SSRF guarded by HMAC verify + urlcheck.Check + GuardedTransport
	if err != nil {
		writeError(w, http.StatusBadGateway, "bad_gateway", "image fetch failed")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, "bad_gateway", "image origin error")
		return
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(ct), "image/") {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "not an image")
		return
	}
	if resp.ContentLength > imageProxyMaxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "image too large")
		return
	}

	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", imageProxyCacheControl)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if resp.ContentLength >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}
	w.WriteHeader(http.StatusOK)
	// LimitReader is the backstop for origins that under-report or omit
	// Content-Length; the explicit ContentLength check above handles honest ones.
	_, _ = io.Copy(w, io.LimitReader(resp.Body, imageProxyMaxBytes))
}
