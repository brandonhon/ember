package feed

import (
	"testing"
)

func TestNormalizeInputURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		{"bare host", "example.com", "https://example.com"},
		{"bare host with path", "example.com/feed.xml", "https://example.com/feed.xml"},
		{"trim and prepend", "  example.com/feed  ", "https://example.com/feed"},
		{"https unchanged", "https://example.com/feed", "https://example.com/feed"},
		{"http upgraded", "http://example.com/feed", "https://example.com/feed"},
		{"http upgraded case-insensitive", "HTTP://example.com/feed", "https://example.com/feed"},
		{"https preserved with port", "https://example.com:8443/feed", "https://example.com:8443/feed"},
		{"http upgraded preserves rest", "http://example.com:80/feed?a=1", "https://example.com:80/feed?a=1"},
		{"other scheme left for downstream reject", "ftp://example.com", "ftp://example.com"},
		{"bare host with port", "example.com:8080/feed", "https://example.com:8080/feed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeInputURL(tc.in)
			if got != tc.want {
				t.Errorf("NormalizeInputURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
