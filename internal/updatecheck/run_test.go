package updatecheck

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const relJSON = `{"tag_name":"v9.9.9","html_url":"https://example.test/rel","published_at":"2026-01-01T00:00:00Z"}`

// runChecker returns a Checker pointed at a stub GitHub, with the first poll
// effectively immediate so the Run loop is exercisable.
func runChecker(t *testing.T, current string, h http.HandlerFunc) (*Checker, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	c := New(current, "owner/name", nil)
	c.baseURL = srv.URL
	c.client = srv.Client()
	c.firstDelay = time.Millisecond
	return c, &hits
}

func waitFor(t *testing.T, cond func() bool, why string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", why)
}

// The loop polls and populates the cache, then exits when ctx is cancelled.
func TestRun_PollsThenStopsOnContextCancel(t *testing.T) {
	c, hits := runChecker(t, "v0.9.4", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(relJSON))
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx, time.Hour, nil); close(done) }()

	waitFor(t, func() bool { _, ok := c.Latest(); return ok }, "the first poll to populate the cache")
	res, _ := c.Latest()
	if res.Latest != "v9.9.9" || !res.Available {
		t.Errorf("result = %+v, want v9.9.9 available", res)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
	if hits.Load() == 0 {
		t.Error("no request was made")
	}
}

// The enabled callback is the admin runtime kill-switch: when it returns false
// the poll is skipped AND the cache is cleared, so /api/me stops advertising
// an update the operator just turned off.
func TestRun_DisabledSkipsPollAndClearsCache(t *testing.T) {
	c, hits := runChecker(t, "v0.9.4", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(relJSON))
	})

	// Seed a cached result the way a previous enabled poll would have.
	if err := c.checkOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Latest(); !ok {
		t.Fatal("precondition: expected a cached result")
	}
	before := hits.Load()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { c.Run(ctx, 10*time.Millisecond, func() bool { return false }); close(done) }()

	waitFor(t, func() bool { _, ok := c.Latest(); return !ok }, "the disabled check to clear the cache")
	if got := hits.Load(); got != before {
		t.Errorf("made %d requests while disabled, want none", got-before)
	}
	cancel()
	<-done
}

// A nil enabled callback means "always enabled" — that is how a deployment
// with the feature simply left on is wired.
func TestRun_NilEnabledMeansEnabled(t *testing.T) {
	c, _ := runChecker(t, "v0.9.4", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(relJSON))
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx, time.Hour, nil)
	waitFor(t, func() bool { _, ok := c.Latest(); return ok }, "a nil callback to be treated as enabled")
}

// A failing poll must not take the loop down — it logs and waits for the next
// interval, so a transient GitHub outage doesn't disable the feature until
// restart.
func TestRun_SurvivesPollFailure(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	c, hits := runChecker(t, "v0.9.4", func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(relJSON))
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx, 5*time.Millisecond, nil)

	waitFor(t, func() bool { return hits.Load() >= 2 }, "the loop to keep polling after a failure")
	fail.Store(false)
	waitFor(t, func() bool { _, ok := c.Latest(); return ok }, "recovery once GitHub responds")
}

// clear() drops the cache; Latest reports not-ok afterwards.
func TestClear_DropsCachedResult(t *testing.T) {
	c, _ := runChecker(t, "v0.9.4", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(relJSON))
	})
	if err := c.checkOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Latest(); !ok {
		t.Fatal("expected a cached result")
	}
	c.clear()
	res, ok := c.Latest()
	if ok || res != (Result{}) {
		t.Errorf("after clear: %+v, ok=%v; want zero and false", res, ok)
	}
}

// The decoder is bounded, so a runaway response is refused rather than read
// into memory in full.
func TestCheckOnce_BoundsTheResponseBody(t *testing.T) {
	c, _ := runChecker(t, "v0.9.4", func(w http.ResponseWriter, _ *http.Request) {
		// Valid JSON prefix followed by megabytes of filler inside a string.
		_, _ = io.WriteString(w, `{"tag_name":"v9.9.9","html_url":"`)
		chunk := strings.Repeat("A", 64*1024)
		for range 40 { // ~2.5 MiB, past the 1 MiB cap
			_, _ = io.WriteString(w, chunk)
		}
		_, _ = io.WriteString(w, `"}`)
	})
	err := c.checkOnce(context.Background())
	if err == nil {
		t.Fatal("an oversize release payload was accepted")
	}
	if _, ok := c.Latest(); ok {
		t.Error("cache was populated from an oversize payload")
	}
	_ = fmt.Sprint(err)
}

// Latest() is documented safe to call concurrently with the Run loop.
func TestLatest_ConcurrentWithRun(t *testing.T) {
	c, _ := runChecker(t, "v0.9.4", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(relJSON))
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx, time.Millisecond, nil)
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		_, _ = c.Latest()
	}
}
