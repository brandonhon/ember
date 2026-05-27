package store

import (
	"context"
	"strconv"
	"strings"
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

	// Default backlog when no env override is set and no DB row exists.
	DefaultInitialBacklogHours = 48
)

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
