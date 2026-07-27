package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLoginFailures_RecordClearAndRead(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()

	lf, err := s.GetLoginFailures(ctx, "alice")
	if err != nil {
		t.Fatalf("unknown username should not error: %v", err)
	}
	if lf.Fails != 0 || lf.LastFailAt != 0 {
		t.Errorf("unknown username = %+v, want zero value", lf)
	}

	for want := 1; want <= 3; want++ {
		got, err := s.RecordLoginFailure(ctx, "alice")
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("RecordLoginFailure returned %d, want %d", got, want)
		}
	}
	lf, err = s.GetLoginFailures(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if lf.Fails != 3 {
		t.Errorf("fails = %d, want 3", lf.Fails)
	}
	if lf.LastFailAt == 0 {
		t.Error("last_fail_at not stamped")
	}

	if err := s.ClearLoginFailures(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if lf, _ := s.GetLoginFailures(ctx, "alice"); lf.Fails != 0 {
		t.Errorf("fails after clear = %d, want 0", lf.Fails)
	}
}

// Throttle state must not bleed between accounts, or one attacker spraying
// "admin" would also throttle everyone else.
func TestLoginFailures_IsolatedPerUsername(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		if _, err := s.RecordLoginFailure(ctx, "alice"); err != nil {
			t.Fatal(err)
		}
	}
	if lf, _ := s.GetLoginFailures(ctx, "bob"); lf.Fails != 0 {
		t.Errorf("bob's fails = %d, want 0", lf.Fails)
	}
}

// Usernames are matched case-sensitively by GetUserByUsername, so the throttle
// must not fold case — that would merge two distinct accounts' state.
func TestLoginFailures_CaseSensitive(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	if _, err := s.RecordLoginFailure(ctx, "Alice"); err != nil {
		t.Fatal(err)
	}
	if lf, _ := s.GetLoginFailures(ctx, "alice"); lf.Fails != 0 {
		t.Error("lowercase variant inherited uppercase failures")
	}
}

// An over-long username must still land in *some* bucket rather than acting as
// an untracked retry lane, and must not store unbounded text.
func TestLoginFailures_LongUsernameTruncatedNotSkipped(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	long := strings.Repeat("a", maxThrottleKeyLen*4)
	for i := 1; i <= 3; i++ {
		n, err := s.RecordLoginFailure(ctx, long)
		if err != nil {
			t.Fatal(err)
		}
		if n != i {
			t.Fatalf("attempt %d recorded as %d — long usernames are escaping the throttle", i, n)
		}
	}
	var stored string
	if err := s.DB.QueryRowContext(ctx, `SELECT username FROM login_failures`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != maxThrottleKeyLen {
		t.Errorf("stored key length = %d, want %d", len(stored), maxThrottleKeyLen)
	}
}

func TestPruneLoginFailures_DropsOnlyStaleRows(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	now := time.Now()
	s.Now = func() time.Time { return now.Add(-48 * time.Hour) }
	if _, err := s.RecordLoginFailure(ctx, "old"); err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return now }
	if _, err := s.RecordLoginFailure(ctx, "recent"); err != nil {
		t.Fatal(err)
	}

	n, err := s.PruneLoginFailures(ctx, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("pruned %d rows, want 1", n)
	}
	if lf, _ := s.GetLoginFailures(ctx, "recent"); lf.Fails != 1 {
		t.Error("prune removed a row still inside its retention window")
	}
	if lf, _ := s.GetLoginFailures(ctx, "old"); lf.Fails != 0 {
		t.Error("stale row survived the prune")
	}
}
