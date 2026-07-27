package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/models"
)

func TestLoginBackoff_Schedule(t *testing.T) {
	tests := []struct {
		fails int
		want  time.Duration
	}{
		{0, 0},
		{1, 0},
		{LoginFreeAttempts - 1, 0}, // last attempt inside the free allowance
		{LoginFreeAttempts, time.Second},
		{LoginFreeAttempts + 1, 2 * time.Second},
		{LoginFreeAttempts + 2, 4 * time.Second},
		{LoginFreeAttempts + 5, 32 * time.Second},
		{LoginFreeAttempts + 6, LoginBackoffCap}, // 64s would exceed the cap
		{LoginFreeAttempts + 100, LoginBackoffCap},
		{1 << 30, LoginBackoffCap}, // shift would overflow without the clamp
	}
	for _, tc := range tests {
		if got := LoginBackoff(tc.fails); got != tc.want {
			t.Errorf("LoginBackoff(%d) = %v, want %v", tc.fails, got, tc.want)
		}
	}
}

// loginUntilThrottled hammers Login with a wrong password and returns how many
// attempts were accepted (i.e. reached credential checking) before the throttle
// kicked in.
func loginUntilThrottled(t *testing.T, a *Auth, username string) (int, time.Duration) {
	t.Helper()
	ctx := context.Background()
	r := httptest.NewRequest(http.MethodPost, "/login", nil)
	for i := 1; i <= LoginFreeAttempts+5; i++ {
		_, err := a.Login(ctx, httptest.NewRecorder(), r, username, "wrong-password")
		if wait, throttled := AsTooManyAttempts(err); throttled {
			return i - 1, wait
		}
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: got %v, want ErrInvalidCredentials", i, err)
		}
	}
	return 0, 0
}

func TestLogin_ThrottlesAfterFreeAttempts(t *testing.T) {
	a := newAuth(t)
	ctx := context.Background()
	hash, _ := a.HashPassword("hunter2")
	if _, err := a.Store.CreateUser(ctx, models.User{Username: "alice", PasswordHash: hash}); err != nil {
		t.Fatal(err)
	}

	accepted, wait := loginUntilThrottled(t, a, "alice")
	if accepted != LoginFreeAttempts {
		t.Errorf("accepted %d attempts before throttling, want %d", accepted, LoginFreeAttempts)
	}
	if wait <= 0 || wait > LoginBackoffCap {
		t.Errorf("retry-after %v out of range (0, %v]", wait, LoginBackoffCap)
	}
}

// The throttle must short-circuit before the password is checked. If it ran
// after verification, a correct password would sail through and the backoff
// would be worthless against an attacker who guesses right on attempt N+1.
func TestLogin_ThrottleBlocksEvenCorrectPassword(t *testing.T) {
	a := newAuth(t)
	ctx := context.Background()
	hash, _ := a.HashPassword("hunter2")
	if _, err := a.Store.CreateUser(ctx, models.User{Username: "alice", PasswordHash: hash}); err != nil {
		t.Fatal(err)
	}
	loginUntilThrottled(t, a, "alice")

	_, err := a.Login(ctx, httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/login", nil),
		"alice", "hunter2")
	if _, throttled := AsTooManyAttempts(err); !throttled {
		t.Fatalf("correct password during backoff: got %v, want throttle", err)
	}
	if !errors.Is(err, ErrTooManyAttempts) {
		t.Error("throttle error does not match ErrTooManyAttempts sentinel")
	}
}

// A username that doesn't exist must throttle identically to one that does —
// otherwise the difference is a free account-enumeration oracle.
func TestLogin_ThrottlesUnknownUsernameToo(t *testing.T) {
	a := newAuth(t)
	accepted, _ := loginUntilThrottled(t, a, "no-such-user")
	if accepted != LoginFreeAttempts {
		t.Errorf("unknown username accepted %d attempts, want %d", accepted, LoginFreeAttempts)
	}
}

func TestLogin_SuccessClearsBackoff(t *testing.T) {
	a := newAuth(t)
	ctx := context.Background()
	hash, _ := a.HashPassword("hunter2")
	if _, err := a.Store.CreateUser(ctx, models.User{Username: "alice", PasswordHash: hash}); err != nil {
		t.Fatal(err)
	}
	// Spend some, but not all, of the free allowance.
	r := httptest.NewRequest(http.MethodPost, "/login", nil)
	for i := 0; i < LoginFreeAttempts-1; i++ {
		if _, err := a.Login(ctx, httptest.NewRecorder(), r, "alice", "nope"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("priming attempt %d: %v", i, err)
		}
	}
	if _, err := a.Login(ctx, httptest.NewRecorder(), r, "alice", "hunter2"); err != nil {
		t.Fatalf("good login: %v", err)
	}
	lf, err := a.Store.GetLoginFailures(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if lf.Fails != 0 {
		t.Errorf("failures after successful login = %d, want 0", lf.Fails)
	}
	// And the full allowance is available again.
	accepted, _ := loginUntilThrottled(t, a, "alice")
	if accepted != LoginFreeAttempts {
		t.Errorf("post-success allowance = %d, want %d", accepted, LoginFreeAttempts)
	}
}

// Once the backoff window has elapsed the account must recover on its own —
// this is what makes it a delay rather than a lockout an attacker can pin.
func TestLogin_BackoffExpiresWithTime(t *testing.T) {
	a := newAuth(t)
	ctx := context.Background()
	hash, _ := a.HashPassword("hunter2")
	if _, err := a.Store.CreateUser(ctx, models.User{Username: "alice", PasswordHash: hash}); err != nil {
		t.Fatal(err)
	}
	_, wait := loginUntilThrottled(t, a, "alice")

	// Advance the auth clock past the backoff window.
	base := time.Now()
	a.Now = func() time.Time { return base.Add(wait + time.Second) }

	if _, err := a.Login(ctx, httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/login", nil),
		"alice", "hunter2"); err != nil {
		t.Fatalf("login after backoff elapsed: %v", err)
	}
}

// A storage failure in the throttle lookup must not take logins down with it.
func TestCheckThrottle_StoreFailureFailsOpen(t *testing.T) {
	a := newAuth(t)
	if err := a.Store.DB.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.checkThrottle(context.Background(), "alice"); err != nil {
		t.Errorf("checkThrottle with a dead store = %v, want nil (fail open)", err)
	}
}

// The argon2 slot bound is the difference between a login flood costing a few
// hundred MiB and costing gigabytes, so assert the ceiling directly.
func TestWithHashSlot_BoundsConcurrency(t *testing.T) {
	var inFlight, peak int64
	var wg sync.WaitGroup
	for i := 0; i < maxConcurrentHashes*4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			withHashSlot(func() {
				n := atomic.AddInt64(&inFlight, 1)
				for {
					old := atomic.LoadInt64(&peak)
					if n <= old || atomic.CompareAndSwapInt64(&peak, old, n) {
						break
					}
				}
				time.Sleep(time.Millisecond)
				atomic.AddInt64(&inFlight, -1)
			})
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt64(&peak); got > maxConcurrentHashes {
		t.Errorf("peak concurrent hashes = %d, want <= %d", got, maxConcurrentHashes)
	}
}
