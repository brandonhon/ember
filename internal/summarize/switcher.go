package summarize

import (
	"context"
	"errors"
	"sync/atomic"
)

// Switcher holds the active backend behind an atomic pointer so an admin can
// change backends at runtime without restarting the process. The poller is
// handed the Switcher once at boot and never learns which backend is behind
// it — which is what keeps the swap a single atomic store instead of a
// coordinated shutdown of the summary worker.
//
// The zero value is usable and means "no backend configured".
type Switcher struct {
	cur atomic.Pointer[Summarizer]
}

// Set swaps the active backend. A nil backend means summarization is
// unconfigured — the state an admin is in after selecting a hosted backend but
// before supplying its API key; Summarize then errors rather than panicking,
// and Configured turns the summary gate off until the configuration is
// finished.
func (s *Switcher) Set(b Summarizer) {
	if b == nil {
		s.cur.Store(nil)
		return
	}
	s.cur.Store(&b)
}

// Get returns the active backend, or nil when none is configured.
func (s *Switcher) Get() Summarizer {
	if p := s.cur.Load(); p != nil {
		return *p
	}
	return nil
}

// Configured reports whether a backend is wired up. This is the "is
// summarization even possible" half of the summary gate; the other half is the
// admin's summaries_enabled setting.
func (s *Switcher) Configured() bool { return s.cur.Load() != nil }

// Summarize delegates to the active backend.
func (s *Switcher) Summarize(ctx context.Context, title, text string) (Result, string, error) {
	b := s.Get()
	if b == nil {
		return Result{}, "", errors.New("summarize: no backend configured")
	}
	return b.Summarize(ctx, title, text)
}
