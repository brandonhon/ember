package feed

import (
	"regexp"
	"strings"
)

// schemePrefix matches a leading URI scheme (e.g. "https://", "ftp://").
var schemePrefix = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]*://`)

// httpPrefix matches a leading "http://" scheme, case-insensitively.
var httpPrefix = regexp.MustCompile(`(?i)^http://`)

// NormalizeInputURL prepares a user-typed feed URL so the user never has to
// type a scheme. It assumes https:
//   - a bare host like "example.com/feed" gets "https://" prepended;
//   - an explicit "http://" is upgraded to "https://";
//   - "https://" (or any other explicit scheme) is left unchanged so the
//     downstream SSRF check can reject non-http(s) schemes.
//
// It is a pure string transform; the result must still pass urlcheck.Check.
func NormalizeInputURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return s
	}
	if httpPrefix.MatchString(s) {
		return "https://" + s[len("http://"):]
	}
	if schemePrefix.MatchString(s) {
		return s
	}
	return "https://" + s
}
