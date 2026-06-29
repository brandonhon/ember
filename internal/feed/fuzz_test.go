package feed

import (
	"context"
	"strings"
	"testing"
)

// FuzzSanitizeHTML asserts the sanitizer never panics and never lets a
// <script> tag through, for arbitrary (malformed) HTML. It runs on every
// ingest path, so robustness here is load-bearing.
func FuzzSanitizeHTML(f *testing.F) {
	f.Add("<p>hi</p><script>alert(1)</script>")
	f.Add(`<img src=x onerror="alert(1)">`)
	f.Add("<<<>>><a href=javascript:1>x</a>")
	f.Add("<svg><script>1</script></svg>")
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		out := SanitizeHTML(s)
		if strings.Contains(strings.ToLower(out), "<script") {
			t.Errorf("sanitized output contains <script: %q -> %q", s, out)
		}
	})
}

// FuzzParse feeds arbitrary bytes to the RSS/Atom parser, which processes feed
// bodies from untrusted publishers. Must never panic.
func FuzzParse(f *testing.F) {
	f.Add([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>t</title>` +
		`<item><title>i</title><link>http://x.test/</link><description>&lt;b&gt;hi&lt;/b&gt;</description></item></channel></rss>`))
	f.Add([]byte(`<feed xmlns="http://www.w3.org/2005/Atom"><title>t</title><entry><title>e</title></entry></feed>`))
	f.Add([]byte(""))
	f.Add([]byte("<rss><channel><item>"))
	f.Fuzz(func(t *testing.T, body []byte) {
		_, _ = Parse(context.Background(), 1, body, "https://example.test/feed")
	})
}

// FuzzSafeHTTPURL asserts the URL guard never returns a non-http(s) string
// (that string is later rendered as an href/src).
func FuzzSafeHTTPURL(f *testing.F) {
	f.Add("https://example.com/a?b=c")
	f.Add("javascript:alert(1)")
	f.Add("data:text/html,x")
	f.Add("http://[::1]:99999")
	f.Add("  HTTPS://EXAMPLE.com  ")
	f.Fuzz(func(t *testing.T, s string) {
		out := SafeHTTPURL(s)
		// Scheme comparison is case-insensitive (url.Parse lowercases the
		// scheme for the check; the returned string keeps its original case,
		// which a browser still treats as http(s) — not a javascript: bypass).
		lower := strings.ToLower(out)
		if out != "" && !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
			t.Errorf("SafeHTTPURL returned non-http(s): %q -> %q", s, out)
		}
	})
}

// FuzzSafeImageURL asserts the image-URL guard only ever returns empty, an
// http(s) URL, or a data:image/ URI — never javascript: or data:text/ (which
// the value is later rendered into an <img src>).
func FuzzSafeImageURL(f *testing.F) {
	f.Add("https://cdn.test/a.jpg")
	f.Add("data:image/png;base64,iVBORw0KGgo=")
	f.Add("data:text/html,<script>alert(1)</script>")
	f.Add("javascript:alert(1)")
	f.Add("  DATA:IMAGE/PNG;base64,x  ")
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		out := strings.ToLower(SafeImageURL(s))
		if out == "" {
			return
		}
		ok := strings.HasPrefix(out, "http://") ||
			strings.HasPrefix(out, "https://") ||
			strings.HasPrefix(out, "data:image/")
		if !ok {
			t.Errorf("SafeImageURL returned a disallowed scheme: %q -> %q", s, out)
		}
	})
}

// FuzzCanonicalURL exercises the dedup canonicalization + cluster hashing over
// arbitrary URL-ish input. Must be panic-free and deterministic (the cluster
// key is a content hash; nondeterminism would scatter duplicates).
func FuzzCanonicalURL(f *testing.F) {
	f.Add("https://Example.com/a/?utm_source=x&id=1#frag")
	f.Add("HTTP://EXAMPLE.COM")
	f.Add("https://x.test//a//b/")
	f.Add("not a url at all")
	f.Add("")
	f.Fuzz(func(t *testing.T, raw string) {
		c1 := CanonicalURL(raw)
		if c2 := CanonicalURL(raw); c1 != c2 {
			t.Errorf("CanonicalURL not deterministic: %q -> %q vs %q", raw, c1, c2)
		}
		if id := ClusterID(c1); ClusterID(c1) != id {
			t.Errorf("ClusterID not deterministic for %q", c1)
		}
	})
}

// FuzzTitleFingerprint exercises the title dedup key over arbitrary titles.
func FuzzTitleFingerprint(f *testing.F) {
	f.Add("Apple Q3 Earnings Beat Estimates")
	f.Add("RE: re: Fwd: breaking news")
	f.Add(strings.Repeat("a ", 4000))
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		if a, b := TitleFingerprint(s), TitleFingerprint(s); a != b {
			t.Errorf("TitleFingerprint not deterministic: %q -> %q vs %q", s, a, b)
		}
	})
}
