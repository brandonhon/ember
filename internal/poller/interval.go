// Package poller schedules and runs feed fetches on a worker pool, persists
// new articles, and enqueues summary jobs.
package poller

import "time"

// Bounds on the adaptive fetch interval.
const (
	MinInterval = 5 * time.Minute
	MaxInterval = 6 * time.Hour
)

// IntervalInputs feeds AdaptiveInterval. All values are from the most recent
// fetch.
type IntervalInputs struct {
	NewArticles int           // count of new articles ingested
	HadError    bool          // last fetch errored
	ErrorCount  int           // consecutive error count
	Current     time.Duration // current configured interval
}

// AdaptiveInterval returns a fresh interval given the last fetch outcome.
// Behavior:
//   - On error, exponentially back off (double) up to MaxInterval.
//   - On a fetch that yielded no new articles, multiply by 1.5 up to MaxInterval.
//   - On a fetch with 1–2 new articles, keep current.
//   - On a fetch with 3+ new articles, halve down to MinInterval.
//
// The result is always clamped to [MinInterval, MaxInterval].
func AdaptiveInterval(in IntervalInputs) time.Duration {
	cur := in.Current
	if cur <= 0 {
		cur = 30 * time.Minute
	}
	var next time.Duration
	switch {
	case in.HadError:
		shift := in.ErrorCount
		if shift < 1 {
			shift = 1
		}
		// Bound the shift so we don't overflow.
		if shift > 10 {
			shift = 10
		}
		next = cur << shift
	case in.NewArticles == 0:
		next = cur * 3 / 2
	case in.NewArticles >= 3:
		next = cur / 2
	default:
		next = cur
	}
	return clamp(next, MinInterval, MaxInterval)
}

func clamp(d, lo, hi time.Duration) time.Duration {
	if d < lo {
		return lo
	}
	if d > hi {
		return hi
	}
	return d
}
