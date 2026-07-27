package auth

import (
	"context"
	"errors"
	"time"
)

// Login throttle policy. The first LoginFreeAttempts consecutive failures for a
// username cost nothing — real people mistype passwords. After that each
// further failure doubles a mandatory wait, starting at LoginBackoffBase and
// capped at LoginBackoffCap.
//
// The cap is deliberately short. A hard lockout would let anyone who knows a
// username deny that account service indefinitely, which on a single-admin
// self-hosted instance means locking the owner out of their own reader. A
// 60-second ceiling instead bounds an attacker to roughly one guess a minute
// per account no matter how many source IPs they have, which is already far
// below what argon2id makes worthwhile, while the worst case for a legitimate
// user being sprayed is a one-minute wait that clears itself.
const (
	LoginFreeAttempts = 5
	LoginBackoffBase  = time.Second
	LoginBackoffCap   = time.Minute
)

// ErrTooManyAttempts is the sentinel for a throttled login. Handlers match it
// with errors.Is; use AsTooManyAttempts to recover the wait duration.
var ErrTooManyAttempts = errors.New("auth: too many login attempts")

// TooManyAttemptsError carries how long the caller must wait before the next
// attempt is accepted, so the handler can emit an accurate Retry-After.
type TooManyAttemptsError struct {
	RetryAfter time.Duration
}

func (e *TooManyAttemptsError) Error() string { return ErrTooManyAttempts.Error() }

// Unwrap makes errors.Is(err, ErrTooManyAttempts) succeed.
func (e *TooManyAttemptsError) Unwrap() error { return ErrTooManyAttempts }

// AsTooManyAttempts returns the throttle wait when err is a throttle rejection.
func AsTooManyAttempts(err error) (time.Duration, bool) {
	var t *TooManyAttemptsError
	if errors.As(err, &t) {
		return t.RetryAfter, true
	}
	return 0, false
}

// LoginBackoff maps a consecutive-failure count to the wait required before the
// next attempt. Returns 0 while the count is within the free allowance.
func LoginBackoff(fails int) time.Duration {
	// fails is the count *before* the attempt being judged, so the boundary is
	// >= rather than >: with LoginFreeAttempts=5, five attempts are let
	// through and the sixth is the first to wait.
	over := fails - LoginFreeAttempts + 1
	if over <= 0 {
		return 0
	}
	// Clamp the exponent before shifting: past the cap the exact value is
	// irrelevant, and an unclamped shift would overflow to nonsense.
	if over > 62 {
		return LoginBackoffCap
	}
	d := LoginBackoffBase << (over - 1)
	if d > LoginBackoffCap || d <= 0 {
		return LoginBackoffCap
	}
	return d
}

// checkThrottle reports whether a login attempt for username may proceed. It
// runs before any user lookup or password hashing, so throttled traffic is
// rejected without touching argon2 — the throttle is a CPU/memory shield as
// much as a guessing shield.
//
// A store error is deliberately non-fatal: failing closed here would turn a
// transient DB hiccup into a total login outage, and the per-IP rate limiter
// plus argon2's own cost still apply.
func (a *Auth) checkThrottle(ctx context.Context, username string) error {
	lf, err := a.Store.GetLoginFailures(ctx, username)
	if err != nil {
		return nil
	}
	wait := LoginBackoff(lf.Fails)
	if wait == 0 {
		return nil
	}
	elapsed := a.Now().Sub(time.Unix(lf.LastFailAt, 0))
	if elapsed >= wait {
		return nil
	}
	remaining := wait - elapsed
	// Round up: a Retry-After of 0 would invite an immediate retry that is
	// still inside the window.
	if remaining < time.Second {
		remaining = time.Second
	}
	return &TooManyAttemptsError{RetryAfter: remaining.Round(time.Second)}
}
