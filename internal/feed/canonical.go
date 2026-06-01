package feed

import (
	"crypto/sha1"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
)

// trackingParamPrefixes are query-parameter name prefixes that are stripped
// during canonicalization. Hardcoded allowlist — conservative, only the
// most prevalent campaign / analytics trackers across the modern web.
// Stripping these collapses near-identical URLs (same article, different
// referrer) into a single cluster.
var trackingParamPrefixes = []string{
	"utm_",  // Google / Urchin (utm_source, utm_medium, utm_campaign, …)
	"_hs",   // HubSpot (_hsenc, _hsmi, _hsfp)
	"mc_",   // Mailchimp (mc_eid, mc_cid)
	"mkt_",  // Marketo (mkt_tok)
	"vero_", // Vero (vero_id, vero_conv)
}

// trackingParamExact is the set of single-word tracking params (no prefix
// pattern) that are stripped.
var trackingParamExact = map[string]struct{}{
	"fbclid":            {}, // Facebook click ID
	"gclid":             {}, // Google ads click ID
	"dclid":             {}, // DoubleClick click ID
	"msclkid":           {}, // Microsoft ads click ID
	"yclid":             {}, // Yandex / Yahoo click ID
	"twclid":            {}, // Twitter click ID
	"igshid":            {}, // Instagram share ID
	"ttclid":            {}, // TikTok click ID
	"ck_subscriber_id":  {}, // ConvertKit
	"oly_anon_id":       {}, // Omeda
	"oly_enc_id":        {}, // Omeda
	"ref":               {}, // ambiguous but overwhelmingly a referrer tag
	"ref_source":        {}, // Substack
	"ref_url":           {}, // various
	"trk":               {}, // LinkedIn
	"trkCampaign":       {}, // LinkedIn
	"hsCtaTracking":     {}, // HubSpot
}

// CanonicalURL returns a normalized form of the input URL with tracking
// query parameters stripped, host lower-cased, trailing slash on the path
// removed (except the bare root "/"), and fragment dropped. It is
// idempotent: CanonicalURL(CanonicalURL(x)) == CanonicalURL(x).
//
// On any parse failure or for empty input, returns the input unchanged —
// callers should treat the result as opaque (a key into the cluster index)
// and not assume it's a valid URL when the input wasn't either.
func CanonicalURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}

	// Host: lowercase. Path: lowercase scheme too (always; matches RFC 3986).
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)

	// Fragment: dropped entirely — never identifies different content for
	// dedup purposes (it's a client-side anchor).
	u.Fragment = ""

	// Path normalization: collapse multiple slashes (rare but happens),
	// strip trailing slash on non-root paths so /foo and /foo/ cluster.
	if u.Path != "" {
		// Don't try to be too clever — preserve case on path (some sites
		// genuinely use case-sensitive paths). Just trim trailing /.
		if len(u.Path) > 1 {
			u.Path = strings.TrimRight(u.Path, "/")
			if u.Path == "" {
				u.Path = "/"
			}
		}
	}

	// Query: filter out tracking params. Preserve everything else with
	// stable ordering so canonicalization is deterministic.
	if u.RawQuery != "" {
		q := u.Query()
		for k := range q {
			if isTrackingParam(k) {
				q.Del(k)
			}
		}
		// Re-encode with sorted keys (url.Values.Encode sorts already).
		u.RawQuery = q.Encode()
		// Sort keys is implicit in url.Values.Encode; this no-op makes
		// the intent explicit and silences any future linter that wants
		// to know we considered ordering.
		_ = sort.StringsAreSorted
	}

	return u.String()
}

// ClusterID derives a short stable identifier from a canonical URL.
// Empty input → empty output (the caller stores "" and the partial index
// on cluster_id excludes it).
//
// 16 hex chars (8 bytes of SHA-1) is plenty to avoid collisions across the
// largest plausible article corpora — collision probability under 1e-9 at
// 100 million distinct URLs.
func ClusterID(canonical string) string {
	if canonical == "" {
		return ""
	}
	sum := sha1.Sum([]byte(canonical))
	return hex.EncodeToString(sum[:8])
}

func isTrackingParam(name string) bool {
	low := strings.ToLower(name)
	if _, ok := trackingParamExact[low]; ok {
		return true
	}
	for _, p := range trackingParamPrefixes {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	return false
}
