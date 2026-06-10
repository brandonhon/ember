package feed

import (
	"net/url"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// sanitizePolicy is bluemonday's user-generated-content policy: it permits
// common formatting, images, and links while stripping <script>, inline event
// handlers (onerror/onload/...), <style>, and javascript:/data: URLs. Feed and
// extracted article HTML is rendered verbatim via {@html} in the reader, so
// every ingest path runs its body through this before it is stored — making
// sanitization a peer of the CSP rather than the CSP being the sole defense.
// RequireNoReferrerOnLinks adds rel="nofollow noreferrer noopener" to every
// anchor, preventing tab-napping via target="_blank" links in feed content.
// Compiled once; bluemonday policies are safe for concurrent use.
var sanitizePolicy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.RequireNoReferrerOnLinks(true)
	return p
}()

// SanitizeHTML strips dangerous markup from an untrusted HTML fragment and
// returns render-safe HTML. Empty in, empty out.
func SanitizeHTML(s string) string {
	if s == "" {
		return ""
	}
	return sanitizePolicy.Sanitize(s)
}

// SafeHTTPURL returns raw only if it parses as an http(s) URL, else "". Use for
// any feed-supplied value that is later rendered as an href/src so javascript:,
// data:, and other non-web schemes can't ride through. Shared by every ingest
// path (feed parse, TT-RSS import) that stores a URL for later rendering.
func SafeHTTPURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	// Require an http(s) scheme AND a host: a scheme-only value like "http:"
	// or an opaque "https:foo" has no host to fetch/link and isn't a usable
	// web URL. (url.Parse lowercases the scheme, so the check is case-
	// insensitive and still rejects javascript:/data:.)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return raw
}

// SafeImageURL is SafeHTTPURL for values rendered as an <img src>. It permits
// http(s) URLs and inline data:image/* URIs (some feeds embed images that way,
// and a data: image is inert as a script vector in an img tag) but drops
// javascript:, vbscript:, and data:text/* so a feed author can't smuggle an
// active-content URL into the image slot.
func SafeImageURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil &&
		(u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
		return raw
	}
	if strings.HasPrefix(strings.ToLower(raw), "data:image/") {
		return raw
	}
	return ""
}
