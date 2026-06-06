package feed

import (
	"testing"
)

func TestCanonicalURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain", "https://example.com/foo", "https://example.com/foo"},
		{"uppercase host", "https://Example.COM/foo", "https://example.com/foo"},
		{"uppercase scheme", "HTTPS://example.com/foo", "https://example.com/foo"},
		{"strip fragment", "https://example.com/foo#section", "https://example.com/foo"},
		{"strip trailing slash", "https://example.com/foo/", "https://example.com/foo"},
		{"preserve root slash", "https://example.com/", "https://example.com/"},
		{"strip utm", "https://example.com/foo?utm_source=twitter", "https://example.com/foo"},
		{"strip multi utm", "https://example.com/foo?utm_source=x&utm_medium=y&utm_campaign=z", "https://example.com/foo"},
		{"strip fbclid", "https://example.com/foo?fbclid=ABC123", "https://example.com/foo"},
		{"strip gclid", "https://example.com/foo?gclid=XYZ", "https://example.com/foo"},
		{"preserve non-tracking params", "https://example.com/foo?id=42&page=2", "https://example.com/foo?id=42&page=2"},
		{"mixed params", "https://example.com/foo?id=42&utm_source=tw&page=2", "https://example.com/foo?id=42&page=2"},
		{"strip prefix family", "https://example.com/foo?_hsenc=abc&_hsmi=def", "https://example.com/foo"},
		{"case-insensitive param name", "https://example.com/foo?UTM_SOURCE=x", "https://example.com/foo"},
		{"keep query order via sort", "https://example.com/foo?b=2&a=1", "https://example.com/foo?a=1&b=2"},
		{"invalid URL returned unchanged", "::nope", "::nope"},
		{"no host returned unchanged", "/just/a/path", "/just/a/path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CanonicalURL(tc.in)
			if got != tc.want {
				t.Errorf("CanonicalURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCanonicalURL_Idempotent(t *testing.T) {
	// Critical property: running canonicalize on the output of canonicalize
	// must be a no-op. Catches accidental round-trip changes (e.g. always-
	// sorted query encoding flipping back and forth).
	inputs := []string{
		"https://Example.COM/Foo/?utm_source=tw&id=1&utm_medium=x#frag",
		"https://blog.example.com/2026/01/article-title/?fbclid=abc",
		"https://news.test/path",
		"https://news.test/path?a=1&b=2",
	}
	for _, in := range inputs {
		once := CanonicalURL(in)
		twice := CanonicalURL(once)
		if once != twice {
			t.Errorf("not idempotent: %q -> %q -> %q", in, once, twice)
		}
	}
}

func TestClusterID(t *testing.T) {
	if got := ClusterID(""); got != "" {
		t.Errorf("ClusterID(\"\") = %q, want empty", got)
	}
	// Deterministic
	a := ClusterID("https://example.com/foo")
	b := ClusterID("https://example.com/foo")
	if a != b {
		t.Errorf("ClusterID not deterministic: %q vs %q", a, b)
	}
	// 16 hex chars
	if len(a) != 16 {
		t.Errorf("ClusterID length = %d, want 16", len(a))
	}
	// Different input → different ID (collision avoidance sanity check).
	if c := ClusterID("https://example.com/bar"); a == c {
		t.Errorf("expected distinct cluster IDs, got %q for both", a)
	}
}

func TestCanonicalURL_ClustersTrackingVariants(t *testing.T) {
	// The whole point of canonicalization: same article, different referrer,
	// must produce the same cluster id.
	urls := []string{
		"https://nyt.example/article/123",
		"https://nyt.example/article/123?utm_source=twitter",
		"https://nyt.example/article/123?utm_source=newsletter&utm_medium=email",
		"https://nyt.example/article/123/?fbclid=ABCD#share",
		"https://NYT.example/article/123?utm_campaign=x&ref_source=hn",
	}
	var first string
	for i, u := range urls {
		canon := CanonicalURL(u)
		cid := ClusterID(canon)
		if i == 0 {
			first = cid
			continue
		}
		if cid != first {
			t.Errorf("expected same cluster for %q, got %q (canon=%q) vs %q", u, cid, canon, first)
		}
	}
}
