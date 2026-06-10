package feed

import (
	"strings"
	"testing"
)

func TestSanitizeHTML(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		mustDrop []string // substrings that must NOT survive
		mustKeep []string // substrings that must survive
	}{
		{
			name:     "strips script tag",
			in:       `<p>hi</p><script>alert(1)</script>`,
			mustDrop: []string{"<script", "alert(1)"},
			mustKeep: []string{"<p>hi</p>"},
		},
		{
			name:     "strips inline event handler",
			in:       `<img src="x" onerror="alert(1)">`,
			mustDrop: []string{"onerror", "alert(1)"},
			mustKeep: []string{"<img", `src="x"`},
		},
		{
			name:     "drops javascript: href",
			in:       `<a href="javascript:alert(1)">x</a>`,
			mustDrop: []string{"javascript:"},
			mustKeep: []string{"x"},
		},
		{
			name:     "keeps benign formatting and links",
			in:       `<p><strong>bold</strong> <a href="https://example.com">link</a></p>`,
			mustKeep: []string{"<strong>bold</strong>", `href="https://example.com"`},
		},
		{
			name:     "empty in empty out",
			in:       "",
			mustKeep: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeHTML(tt.in)
			for _, d := range tt.mustDrop {
				if strings.Contains(got, d) {
					t.Errorf("sanitized output still contains %q: %q", d, got)
				}
			}
			for _, k := range tt.mustKeep {
				if !strings.Contains(got, k) {
					t.Errorf("sanitized output dropped %q: %q", k, got)
				}
			}
		})
	}
}

func TestSafeHTTPURL(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"https://example.com", "https://example.com"},
		{"http://example.com/a?b=c", "http://example.com/a?b=c"},
		{"  https://example.com  ", "https://example.com"},
		{"javascript:alert(1)", ""},
		{"data:text/html,<script>alert(1)</script>", ""},
		{"ftp://example.com", ""},
		{"//example.com", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := SafeHTTPURL(tt.in); got != tt.want {
			t.Errorf("SafeHTTPURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSafeImageURL(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"https://example.com/a.png", "https://example.com/a.png"},
		{"http://example.com/a.jpg?v=2", "http://example.com/a.jpg?v=2"},
		{"  https://example.com/a.png  ", "https://example.com/a.png"},
		// data:image is allowed (inline images) and preserves original casing.
		{"data:image/png;base64,iVBORw0KGgo=", "data:image/png;base64,iVBORw0KGgo="},
		{"DATA:IMAGE/PNG;base64,iVBORw0KGgo=", "DATA:IMAGE/PNG;base64,iVBORw0KGgo="},
		// Script-y and non-image data URIs are dropped.
		{"javascript:alert(1)", ""},
		{"vbscript:msgbox(1)", ""},
		{"data:text/html,<script>alert(1)</script>", ""},
		{"//example.com/a.png", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := SafeImageURL(tt.in); got != tt.want {
			t.Errorf("SafeImageURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
