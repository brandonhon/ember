// Package emailinbox terminates inbound SMTP for the per-user email
// newsletter feature. Mail addressed to <handle>@<EMBER_EMAIL_DOMAIN>
// is parsed into a synthetic Article and ingested through the existing
// store path — the user sees newsletters in the same list as RSS items.
package emailinbox

import (
	"crypto/rand"
	"fmt"
)

// handleAlphabet is Crockford-style base32: no I/L/O/U to avoid
// ambiguous characters in handles users might transcribe. 32 distinct
// chars → 5 bits per char → 12 chars carry ~60 bits of entropy.
const handleAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// handleLen is the fixed character length of a generated handle.
const handleLen = 12

// GenerateHandle relies on `byte % len(handleAlphabet)` being bias-free,
// which holds iff len(handleAlphabet) is a power of two (256 is divisible
// by it). This compile-time assertion breaks the build if the alphabet
// ever drifts to a non-power-of-two size — otherwise the bias would
// reappear silently. The index is 0 for power-of-two lengths (valid) and
// out of bounds otherwise (compile error).
var _ = [1]struct{}{}[len(handleAlphabet)&(len(handleAlphabet)-1)]

// GenerateHandle returns a fresh ~60-bit random handle.
func GenerateHandle() (string, error) {
	buf := make([]byte, handleLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("emailinbox: read random: %w", err)
	}
	for i, b := range buf {
		buf[i] = handleAlphabet[int(b)%len(handleAlphabet)]
	}
	return string(buf), nil
}

// ValidHandle returns true if s is exactly handleLen characters drawn
// from the handle alphabet. Used to filter envelope-To addresses
// without ever hitting the DB.
func ValidHandle(s string) bool {
	if len(s) != handleLen {
		return false
	}
	for _, r := range s {
		if !inAlphabet(byte(r)) {
			return false
		}
	}
	return true
}

func inAlphabet(b byte) bool {
	for i := 0; i < len(handleAlphabet); i++ {
		if handleAlphabet[i] == b {
			return true
		}
	}
	return false
}
