package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/brandonhon/ember/internal/digest"
	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// runDigestSender ticks every 5 minutes and dispatches user digest emails
// whose scheduled UTC hour/minute is at or just past the current clock.
// Stops on ctx.Done().
//
// fallback holds the env-derived SMTP defaults; each tick re-resolves the
// live config from app_settings so an admin edit takes effect on the next
// tick without a process restart.
func runDigestSender(ctx context.Context, st *store.Store, sender *digest.Sender, fallback store.SMTPSettings, lg *slog.Logger) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	// First tick immediately so restarts don't strand a missed send.
	tickDigest(ctx, st, sender, fallback, lg)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tickDigest(ctx, st, sender, fallback, lg)
		}
	}
}

func tickDigest(ctx context.Context, st *store.Store, sender *digest.Sender, fallback store.SMTPSettings, lg *slog.Logger) {
	// Refresh the sender's SMTP config from the live store each tick so admin
	// edits via /api/admin/settings flow through without a restart. Fallback
	// is the env-derived config; app_settings rows override.
	live := st.ResolveSMTPSettings(ctx, fallback)
	sender.SMTP = digest.SMTPConfig{
		Host: live.Host, Port: live.Port,
		Username: live.Username, Password: live.Password,
		From: live.From, StartTLS: live.StartTLS,
	}
	if !sender.SMTP.Configured() {
		return
	}
	digests, err := st.ListEnabledDigests(ctx)
	if err != nil {
		lg.Warn("digest: list enabled", "err", err)
		return
	}
	now := time.Now().UTC()
	for _, d := range digests {
		if !dueDigest(d, now) {
			continue
		}
		u, err := st.GetUser(ctx, d.UserID)
		if err != nil {
			lg.Warn("digest: load user", "user_id", d.UserID, "err", err)
			continue
		}
		n, err := sender.SendForUser(ctx, u, d)
		if err != nil {
			lg.Warn("digest: send failed", "user_id", d.UserID, "err", err)
			// Still mark sent so we don't hammer a broken SMTP every tick.
			_ = st.MarkDigestSent(ctx, d.UserID, now.Unix())
			continue
		}
		if n == 0 {
			lg.Debug("digest: no articles", "user_id", d.UserID)
			_ = st.MarkDigestSent(ctx, d.UserID, now.Unix())
			continue
		}
		lg.Info("digest sent", "user_id", d.UserID, "articles", n)
		_ = st.MarkDigestSent(ctx, d.UserID, now.Unix())
	}
}

// dueDigest is true when the wall-clock has crossed today's scheduled time
// and we haven't sent already today. Granularity is the 5-minute ticker,
// so the send fires within ~5 min of the configured hour/minute.
func dueDigest(d models.UserDigest, now time.Time) bool {
	if !d.Enabled {
		return false
	}
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	scheduled := dayStart.Add(time.Duration(d.HourUTC)*time.Hour + time.Duration(d.MinuteUTC)*time.Minute)
	// Already sent today? Skip.
	if d.LastSentAt >= dayStart.Unix() {
		return false
	}
	return !now.Before(scheduled)
}
