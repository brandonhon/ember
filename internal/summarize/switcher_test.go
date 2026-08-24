package summarize

import (
	"context"
	"testing"
)

// The Switcher is swapped by an admin HTTP handler while the poller's summary
// worker is calling Summarize through it. That is the whole reason it exists,
// so the test runs both at once under -race; a plain interface field would
// pass every assertion below and still be a data race.
func TestSwitcher_DelegatesAndSwapsUnderConcurrency(t *testing.T) {
	var s Switcher
	if _, _, err := s.Summarize(context.Background(), "t", "b"); err == nil {
		t.Fatal("a switcher with no backend must error, not panic")
	}
	if s.Configured() {
		t.Fatal("the zero value must report itself unconfigured")
	}
	if s.Get() != nil {
		t.Fatal("Get on the zero value must be nil")
	}

	s.Set(Noop{})
	if _, model, err := s.Summarize(context.Background(), "t", "b"); err != nil || model != "noop" {
		t.Fatalf("model = %q err = %v", model, err)
	}
	if !s.Configured() {
		t.Fatal("Configured should be true once a backend is set")
	}

	// A nil backend means "not configured yet" — an admin has selected a
	// backend but not finished supplying its credentials. It must unwire
	// cleanly rather than store a non-nil interface holding a nil pointer.
	s.Set(nil)
	if s.Configured() {
		t.Fatal("Set(nil) must clear the backend")
	}
	if _, _, err := s.Summarize(context.Background(), "t", "b"); err == nil {
		t.Fatal("Summarize after Set(nil) must error")
	}

	s.Set(Noop{})
	done := make(chan struct{})
	go func() {
		for range 200 {
			_, _, _ = s.Summarize(context.Background(), "t", "b")
		}
		close(done)
	}()
	for range 200 {
		s.Set(Noop{})
	}
	<-done
}
