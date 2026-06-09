// Package poller schedules and runs feed fetches on a worker pool, persists
// new articles, and enqueues summary jobs.
package poller

import "time"

// MaxInterval is the adaptive back-off ceiling for quiet feeds. The floor is
// runtime-configurable (admin setting / EMBER_POLL_MIN_INTERVAL) and passed
// into AdaptiveInterval per fetch; see store.DefaultPollMinInterval and the
// store.PollMinInterval{Floor,Ceil} hard bounds.
const MaxInterval = 6 * time.Hour

// fallbackMinInterval is a defensive default used only when a zero/empty floor
// reaches AdaptiveInterval (should not happen in production, where the poller
// resolves a clamped value from the store).
const fallbackMinInterval = 30 * time.Minute

// IntervalInputs feeds AdaptiveInterval. All values are from the most recent
// fetch.
type IntervalInputs struct {
	NewArticles int           // count of new articles ingested
	HadError    bool          // last fetch errored
	ErrorCount  int           // consecutive error count
	Current     time.Duration // current configured interval
}

// AdaptiveInterval returns a fresh interval given the last fetch outcome,
// clamped to [minIv, maxIv]. minIv is the admin-configured floor; maxIv is the
// back-off ceiling (raised to minIv when a high floor would otherwise exceed
// it). Behavior:
//   - On error, exponentially back off (double) up to maxIv.
//   - On a fetch that yielded no new articles, multiply by 1.5 up to maxIv.
//   - On a fetch with 1–2 new articles, keep current.
//   - On a fetch with 3+ new articles, halve down to minIv.
func AdaptiveInterval(in IntervalInputs, minIv, maxIv time.Duration) time.Duration {
	if minIv <= 0 {
		minIv = fallbackMinInterval
	}
	if maxIv < minIv {
		maxIv = minIv
	}
	cur := in.Current
	if cur <= 0 {
		cur = minIv
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
	return clamp(next, minIv, maxIv)
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
