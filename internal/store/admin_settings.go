package store

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// app_settings keys for runtime-mutable admin-edited values. Env vars supply
// the boot-time defaults; admin edits via the UI persist here and override.
const (
	keySMTPHost     = "smtp_host"
	keySMTPPort     = "smtp_port"
	keySMTPUser     = "smtp_user"
	keySMTPPassword = "smtp_password"
	keySMTPFrom     = "smtp_from"
	keySMTPStartTLS = "smtp_starttls"

	// Initial backlog window applied on a feed's very first ingest. Articles
	// published more than N hours ago are dropped. 0 disables the gate. The
	// motivation: adding a feed shouldn't dump months of history into your
	// reader. Subsequent polls of the same feed never apply this gate.
	keyInitialBacklogHours = "initial_backlog_hours"

	// Default backlog when no env override is set and no DB row exists. A new
	// feed pulls only the last 24h on its first fetch.
	DefaultInitialBacklogHours = 24

	// Reading-view window: Today / a feed / a category / All Unread only show
	// (and count) articles published within this many hours. Admin-configurable
	// up to the retention cap. Default 24h.
	keyReadingWindowHours     = "reading_window_hours"
	DefaultReadingWindowHours = 24

	// Search window: full-text search only matches articles published within
	// this many hours. Default 48h, extendable up to the retention cap.
	keySearchWindowHours     = "search_window_hours"
	DefaultSearchWindowHours = 48

	// Floor for the adaptive per-feed fetch interval (the "check feeds every…"
	// knob). Admin-configurable via the UI / EMBER_POLL_MIN_INTERVAL, clamped
	// to [PollMinIntervalFloor, PollMinIntervalCeil]. The default (30m) gives
	// readers time to work through Fresh without new items piling in.
	keyPollMinIntervalSeconds = "poll_min_interval_seconds"
)

// Bounds + default for the admin-configurable adaptive-fetch floor. Canonical
// here so the poller, the env-var parser, and the settings API all agree.
const (
	DefaultPollMinInterval = 30 * time.Minute
	PollMinIntervalFloor   = 5 * time.Minute
	PollMinIntervalCeil    = 24 * time.Hour
)

// RetentionHours is the fixed rolling retention window. Articles older than
// this are pruned from the database (except starred / read-later / pinned /
// shared). It is the hard ceiling for both the reading-window and search-
// window settings — you can't surface what's already been pruned. Not
// admin-configurable by design.
const RetentionHours = 7 * 24

// Bounds for the two window settings. Floor is 24h (the unread/count logic
// assumes at least a day); the ceiling is the retention window.
const (
	WindowHoursFloor = 24
	WindowHoursCeil  = RetentionHours
)

func clampInt(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// ResolveReadingWindowHours returns the effective reading-view window in hours
// (DB row wins, clamped to [WindowHoursFloor, WindowHoursCeil]), else fallback.
func (s *Store) ResolveReadingWindowHours(ctx context.Context, fallback int) int {
	if v, _ := s.GetAppSetting(ctx, keyReadingWindowHours); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return clampInt(n, WindowHoursFloor, WindowHoursCeil)
		}
	}
	return clampInt(fallback, WindowHoursFloor, WindowHoursCeil)
}

// PutReadingWindowHours persists the reading-view window (clamped).
func (s *Store) PutReadingWindowHours(ctx context.Context, n int) error {
	n = clampInt(n, WindowHoursFloor, WindowHoursCeil)
	return s.PutAppSetting(ctx, keyReadingWindowHours, strconv.Itoa(n))
}

// ResolveSearchWindowHours returns the effective search window in hours (DB
// row wins, clamped to [WindowHoursFloor, WindowHoursCeil]), else fallback.
func (s *Store) ResolveSearchWindowHours(ctx context.Context, fallback int) int {
	if v, _ := s.GetAppSetting(ctx, keySearchWindowHours); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return clampInt(n, WindowHoursFloor, WindowHoursCeil)
		}
	}
	return clampInt(fallback, WindowHoursFloor, WindowHoursCeil)
}

// PutSearchWindowHours persists the search window (clamped).
func (s *Store) PutSearchWindowHours(ctx context.Context, n int) error {
	n = clampInt(n, WindowHoursFloor, WindowHoursCeil)
	return s.PutAppSetting(ctx, keySearchWindowHours, strconv.Itoa(n))
}

// ResolvePollMinInterval returns the admin-set minimum fetch interval from
// app_settings (clamped to the hard bounds), or fallback when unset/invalid.
func (s *Store) ResolvePollMinInterval(ctx context.Context, fallback time.Duration) time.Duration {
	if v, _ := s.GetAppSetting(ctx, keyPollMinIntervalSeconds); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return clampDuration(time.Duration(n)*time.Second, PollMinIntervalFloor, PollMinIntervalCeil)
		}
	}
	return fallback
}

// PutPollMinInterval persists the minimum fetch interval (clamped to the hard
// bounds), stored as whole seconds.
func (s *Store) PutPollMinInterval(ctx context.Context, d time.Duration) error {
	d = clampDuration(d, PollMinIntervalFloor, PollMinIntervalCeil)
	return s.PutAppSetting(ctx, keyPollMinIntervalSeconds, strconv.FormatInt(int64(d.Seconds()), 10))
}

func clampDuration(d, lo, hi time.Duration) time.Duration {
	if d < lo {
		return lo
	}
	if d > hi {
		return hi
	}
	return d
}

// SMTPSettings mirrors the shape of digest.SMTPConfig — kept here so the store
// layer doesn't import digest (would be a backwards dependency). cmd/ember and
// the api handlers translate between the two shapes.
type SMTPSettings struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	StartTLS bool
}

// ResolveSMTPSettings returns the effective SMTP config: app_settings rows
// override fields in the fallback (env-derived) settings. An empty app_settings
// value for a string field means "use fallback"; for booleans/ints an empty
// row also falls back. This lets an admin edit only some fields and inherit
// the rest from .env.
func (s *Store) ResolveSMTPSettings(ctx context.Context, fallback SMTPSettings) SMTPSettings {
	out := fallback
	if v, _ := s.GetAppSetting(ctx, keySMTPHost); v != "" {
		out.Host = v
	}
	if v, _ := s.GetAppSetting(ctx, keySMTPPort); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			out.Port = n
		}
	}
	if v, _ := s.GetAppSetting(ctx, keySMTPUser); v != "" {
		out.Username = v
	}
	if v, _ := s.GetAppSetting(ctx, keySMTPPassword); v != "" {
		out.Password = v
	}
	if v, _ := s.GetAppSetting(ctx, keySMTPFrom); v != "" {
		out.From = v
	}
	if v, _ := s.GetAppSetting(ctx, keySMTPStartTLS); v != "" {
		// Accept "1"/"0"/"true"/"false".
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			out.StartTLS = true
		case "0", "false", "no", "off":
			out.StartTLS = false
		}
	}
	return out
}

// SMTPUpdate is the pointer-bag the admin UI sends in. Nil = no change; an
// empty string clears the override (falls back to env). Password has an
// extra flag because "" means "no change" for that field — admins typically
// don't want to round-trip the existing password back to the SPA.
type SMTPUpdate struct {
	Host          *string
	Port          *int
	Username      *string
	Password      *string
	ClearPassword bool
	From          *string
	StartTLS      *bool
}

// PutSMTPSettings persists the supplied updates. Nil pointers are skipped,
// matching the branding handler's "patch only what's provided" convention.
func (s *Store) PutSMTPSettings(ctx context.Context, u SMTPUpdate) error {
	if u.Host != nil {
		if err := s.PutAppSetting(ctx, keySMTPHost, strings.TrimSpace(*u.Host)); err != nil {
			return err
		}
	}
	if u.Port != nil {
		if err := s.PutAppSetting(ctx, keySMTPPort, strconv.Itoa(*u.Port)); err != nil {
			return err
		}
	}
	if u.Username != nil {
		if err := s.PutAppSetting(ctx, keySMTPUser, strings.TrimSpace(*u.Username)); err != nil {
			return err
		}
	}
	// Password write rules:
	//   - ClearPassword=true → explicit clear (write empty string).
	//   - Password != nil and != ""   → store the new value.
	//   - Password == nil, ClearPassword=false → no change.
	if u.ClearPassword {
		if err := s.PutAppSetting(ctx, keySMTPPassword, ""); err != nil {
			return err
		}
	} else if u.Password != nil && *u.Password != "" {
		if err := s.PutAppSetting(ctx, keySMTPPassword, *u.Password); err != nil {
			return err
		}
	}
	if u.From != nil {
		if err := s.PutAppSetting(ctx, keySMTPFrom, strings.TrimSpace(*u.From)); err != nil {
			return err
		}
	}
	if u.StartTLS != nil {
		v := "0"
		if *u.StartTLS {
			v = "1"
		}
		if err := s.PutAppSetting(ctx, keySMTPStartTLS, v); err != nil {
			return err
		}
	}
	return nil
}

// ResolveBacklogHours returns the effective initial-ingest backlog window in
// hours. The DB row wins if set; otherwise the env-derived fallback (typically
// DefaultInitialBacklogHours). 0 means "no gate" — feeds ingest their full
// upstream history on first fetch.
func (s *Store) ResolveBacklogHours(ctx context.Context, fallback int) int {
	if v, _ := s.GetAppSetting(ctx, keyInitialBacklogHours); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return fallback
}

// PutBacklogHours persists the new window. Negative inputs are rejected by
// the caller's HTTP-layer validation; we coerce here for safety.
func (s *Store) PutBacklogHours(ctx context.Context, n int) error {
	if n < 0 {
		n = 0
	}
	return s.PutAppSetting(ctx, keyInitialBacklogHours, strconv.Itoa(n))
}
