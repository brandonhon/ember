package store

import (
	"context"
	"testing"
)

func TestResolveSMTPSettings_FallbackWhenUnset(t *testing.T) {
	s := NewTest(t)
	fb := SMTPSettings{
		Host: "env.smtp.test", Port: 587, Username: "u",
		Password: "envpw", From: "ember@env", StartTLS: true,
	}
	got := s.ResolveSMTPSettings(context.Background(), fb)
	if got != fb {
		t.Errorf("with no app_settings rows, resolved config should equal fallback; got %+v want %+v", got, fb)
	}
}

func TestResolveSMTPSettings_OverridesFromStore(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	fb := SMTPSettings{Host: "env", Port: 587, From: "env@x", StartTLS: true}

	// Override host, port, and STARTTLS via the typed setter; leave the rest
	// alone — fallback should still fill them in.
	host := "db.smtp.test"
	port := 2525
	starttls := false
	if err := s.PutSMTPSettings(ctx, SMTPUpdate{Host: &host, Port: &port, StartTLS: &starttls}); err != nil {
		t.Fatal(err)
	}
	got := s.ResolveSMTPSettings(ctx, fb)
	if got.Host != host || got.Port != port || got.StartTLS != starttls {
		t.Errorf("override missed: got %+v", got)
	}
	if got.From != fb.From {
		t.Errorf("untouched field should fall back: got From=%q want %q", got.From, fb.From)
	}
}

func TestPutSMTPSettings_PasswordRules(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	pw := "secret123"
	if err := s.PutSMTPSettings(ctx, SMTPUpdate{Password: &pw}); err != nil {
		t.Fatal(err)
	}
	if got := s.ResolveSMTPSettings(ctx, SMTPSettings{}); got.Password != pw {
		t.Errorf("password not stored: got %q want %q", got.Password, pw)
	}
	// Empty-pointer password does NOT clear the stored value — that requires
	// the explicit clear flag.
	empty := ""
	if err := s.PutSMTPSettings(ctx, SMTPUpdate{Password: &empty}); err != nil {
		t.Fatal(err)
	}
	if got := s.ResolveSMTPSettings(ctx, SMTPSettings{}); got.Password != pw {
		t.Errorf("empty password should be a no-op; got %q want %q", got.Password, pw)
	}
	// ClearPassword: true does the actual clear.
	if err := s.PutSMTPSettings(ctx, SMTPUpdate{ClearPassword: true}); err != nil {
		t.Fatal(err)
	}
	if got := s.ResolveSMTPSettings(ctx, SMTPSettings{}); got.Password != "" {
		t.Errorf("clear flag should clear; got %q", got.Password)
	}
}

func TestResolveBacklogHours_DefaultsAndOverride(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	if got := s.ResolveBacklogHours(ctx, 48); got != 48 {
		t.Errorf("unset → fallback; got %d want 48", got)
	}
	if err := s.PutBacklogHours(ctx, 24); err != nil {
		t.Fatal(err)
	}
	if got := s.ResolveBacklogHours(ctx, 48); got != 24 {
		t.Errorf("override; got %d want 24", got)
	}
	if err := s.PutBacklogHours(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if got := s.ResolveBacklogHours(ctx, 48); got != 0 {
		t.Errorf("zero is a valid 'no gate' override; got %d want 0", got)
	}
	if err := s.PutBacklogHours(ctx, -5); err != nil {
		t.Fatal(err)
	}
	if got := s.ResolveBacklogHours(ctx, 48); got != 0 {
		t.Errorf("negative gets coerced to zero; got %d want 0", got)
	}
}
