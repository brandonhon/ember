package feed

import (
	"strings"
	"unicode"
)

// stopWords are the high-frequency English words that contribute no signal
// to a title fingerprint. Kept small — aggressive stripping creates more
// collisions than it eliminates noise. Sourced from common stoplists,
// trimmed to the most common ~25.
var stopWords = map[string]struct{}{
	"a": {}, "an": {}, "the": {},
	"and": {}, "or": {}, "but": {},
	"of": {}, "to": {}, "in": {}, "on": {}, "for": {}, "with": {}, "by": {}, "from": {}, "at": {}, "as": {},
	"is": {}, "are": {}, "was": {}, "were": {}, "be": {}, "been": {}, "being": {},
}

// minFingerprintLen rejects fingerprints shorter than this many runes —
// "Re:" or "Hi" or "News" alone aren't usefully distinctive and would
// over-collapse unrelated stories. Drops to empty string instead.
const minFingerprintLen = 8

// TitleFingerprint returns a normalized form of an article title for use
// as a soft equality key during cross-feed dedup. Returns "" when the
// title is too short / too generic to be a reliable cluster key.
//
// Transform:
//  1. Lowercase
//  2. Replace every non-letter / non-digit run with a single space
//  3. Drop tokens in the stopword list
//  4. Trim, collapse whitespace
//  5. Empty out if the result is shorter than minFingerprintLen
//
// Idempotent: TitleFingerprint(TitleFingerprint(x)) == TitleFingerprint(x)
// (assuming the input is the fingerprint of a longer title — otherwise it
// rejects via the length floor).
//
// Examples:
//
//	"The Best Hack of 2026"  -> "best hack 2026"
//	"Best hack of 2026!"     -> "best hack 2026"
//	"Re:"                    -> ""
func TitleFingerprint(title string) string {
	if title == "" {
		return ""
	}
	// Single pass: lowercase + strip non-alphanum to spaces.
	var b strings.Builder
	b.Grow(len(title))
	prevSpace := true
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
			prevSpace = false
		} else if !prevSpace {
			b.WriteByte(' ')
			prevSpace = true
		}
	}
	tokens := strings.Fields(b.String())
	if len(tokens) == 0 {
		return ""
	}
	// Filter stopwords.
	kept := tokens[:0]
	for _, t := range tokens {
		if _, drop := stopWords[t]; drop {
			continue
		}
		kept = append(kept, t)
	}
	if len(kept) == 0 {
		return ""
	}
	out := strings.Join(kept, " ")
	// Count runes for the length floor (unicode-safe).
	n := 0
	for range out {
		n++
		if n >= minFingerprintLen {
			break
		}
	}
	if n < minFingerprintLen {
		return ""
	}
	return out
}
