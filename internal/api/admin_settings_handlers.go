package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/digest"
	"github.com/brandonhon/ember/internal/store"
)

// adminSettings is what the Settings UI reads. The SMTP password is never
// echoed back — instead we send a boolean so the UI can show "stored ✓" and
// offer a Clear button. The "fresh"-style defaults (initial_backlog_hours)
// are reflected so the UI can pre-fill.
type adminSettings struct {
	SMTP struct {
		Host        string `json:"host"`
		Port        int    `json:"port"`
		Username    string `json:"username"`
		PasswordSet bool   `json:"password_set"`
		From        string `json:"from"`
		StartTLS    bool   `json:"starttls"`
	} `json:"smtp"`
	InitialBacklogHours int `json:"initial_backlog_hours"`
	// PollMinIntervalSeconds is the adaptive fetch-interval floor ("check feeds
	// every…"), in seconds. The floor/ceil are echoed so the UI can bound its
	// control without hardcoding them.
	PollMinIntervalSeconds      int `json:"poll_min_interval_seconds"`
	PollMinIntervalFloorSeconds int `json:"poll_min_interval_floor_seconds"`
	PollMinIntervalCeilSeconds  int `json:"poll_min_interval_ceil_seconds"`
}

func (d *Dependencies) handleGetAdminSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	smtp := d.Store.ResolveSMTPSettings(ctx, d.SMTPFallback)
	backlog := d.Store.ResolveBacklogHours(ctx, d.InitialBacklogHoursFallback)

	var out adminSettings
	out.SMTP.Host = smtp.Host
	out.SMTP.Port = smtp.Port
	out.SMTP.Username = smtp.Username
	out.SMTP.PasswordSet = smtp.Password != ""
	out.SMTP.From = smtp.From
	out.SMTP.StartTLS = smtp.StartTLS
	out.InitialBacklogHours = backlog
	out.PollMinIntervalSeconds = int(d.Store.ResolvePollMinInterval(ctx, d.PollMinIntervalFallback).Seconds())
	out.PollMinIntervalFloorSeconds = int(store.PollMinIntervalFloor.Seconds())
	out.PollMinIntervalCeilSeconds = int(store.PollMinIntervalCeil.Seconds())
	writeData(w, http.StatusOK, out, nil)
}

// setAdminSettingsReq is a pointer-bag so only fields the caller sends get
// updated — matches the branding handler's patch semantics.
type setAdminSettingsReq struct {
	SMTP *struct {
		Host          *string `json:"host,omitempty"`
		Port          *int    `json:"port,omitempty"`
		Username      *string `json:"username,omitempty"`
		Password      *string `json:"password,omitempty"`
		ClearPassword bool    `json:"clear_password,omitempty"`
		From          *string `json:"from,omitempty"`
		StartTLS      *bool   `json:"starttls,omitempty"`
	} `json:"smtp,omitempty"`
	InitialBacklogHours    *int `json:"initial_backlog_hours,omitempty"`
	PollMinIntervalSeconds *int `json:"poll_min_interval_seconds,omitempty"`
}

func (d *Dependencies) handleSetAdminSettings(w http.ResponseWriter, r *http.Request) {
	var req setAdminSettingsReq
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	if req.SMTP != nil {
		u := store.SMTPUpdate{
			Host:          req.SMTP.Host,
			Port:          req.SMTP.Port,
			Username:      req.SMTP.Username,
			Password:      req.SMTP.Password,
			ClearPassword: req.SMTP.ClearPassword,
			From:          req.SMTP.From,
			StartTLS:      req.SMTP.StartTLS,
		}
		if err := d.Store.PutSMTPSettings(ctx, u); err != nil {
			internalError(w, "internal", err)
			return
		}
	}
	if req.InitialBacklogHours != nil {
		n := *req.InitialBacklogHours
		if n < 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "initial_backlog_hours must be >= 0")
			return
		}
		if err := d.Store.PutBacklogHours(ctx, n); err != nil {
			internalError(w, "internal", err)
			return
		}
	}
	if req.PollMinIntervalSeconds != nil {
		iv := time.Duration(*req.PollMinIntervalSeconds) * time.Second
		if iv < store.PollMinIntervalFloor || iv > store.PollMinIntervalCeil {
			writeError(w, http.StatusBadRequest, "bad_request",
				"poll_min_interval_seconds must be between "+
					strconv.Itoa(int(store.PollMinIntervalFloor.Seconds()))+" and "+
					strconv.Itoa(int(store.PollMinIntervalCeil.Seconds())))
			return
		}
		if err := d.Store.PutPollMinInterval(ctx, iv); err != nil {
			internalError(w, "internal", err)
			return
		}
	}
	// Echo the resolved (post-update) view so the UI can reconcile.
	d.handleGetAdminSettings(w, r)
}

type testEmailReq struct {
	To string `json:"to"`
}

// handleTestEmail sends a minimal multipart/alternative message to the
// supplied recipient (or the admin's own email if blank). Uses the live SMTP
// config (env fallback overlaid with app_settings) — same path the digest
// sender uses, so a passing test is a real green light.
func (d *Dependencies) handleTestEmail(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())

	var req testEmailReq
	if r.ContentLength > 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}
	to := strings.TrimSpace(req.To)
	if to == "" {
		to = u.Email
	}
	if to == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "no recipient (set ?to= or set your account email)")
		return
	}

	live := d.Store.ResolveSMTPSettings(r.Context(), d.SMTPFallback)
	cfg := digest.SMTPConfig{
		Host: live.Host, Port: live.Port,
		Username: live.Username, Password: live.Password,
		From: live.From, StartTLS: live.StartTLS,
	}
	if !cfg.Configured() {
		writeError(w, http.StatusBadRequest, "smtp_not_configured", "SMTP host, port, and from are all required")
		return
	}

	if err := digest.SendTestMessage(cfg, to, "Ember"); err != nil {
		// Log the full error server-side for diagnosis; return a generic message.
		// Raw net/smtp / TLS errors can carry server banners, internal
		// hostnames, or AUTH-exchange fragments that shouldn't cross the API
		// boundary even to an admin. 502 (not 500) since the upstream relay /
		// config is the likely culprit, not ember itself.
		slog.Default().Warn("smtp test send failed", "host", cfg.Host, "port", cfg.Port, "err", err)
		writeError(w, http.StatusBadGateway, "smtp_send_failed",
			"SMTP test failed — check the server logs for details (auth, DNS, or TLS).")
		return
	}
	writeData(w, http.StatusOK, map[string]any{"sent_to": to}, nil)
}
