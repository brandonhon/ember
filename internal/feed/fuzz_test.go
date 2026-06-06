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
