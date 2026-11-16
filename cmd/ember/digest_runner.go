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
	retry := map[int64]time.Time{}
	// First tick immediately so restarts don't strand a missed send.
	tickDigest(ctx, st, sender, fallback, lg, time.Now().UTC(), retry)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tickDigest(ctx, st, sender, fallback, lg, time.Now().UTC(), retry)
		}
	}
}

// digestRetryAfter spaces out retries after a failed send. A failed digest is
// not marked sent (its articles would otherwise be skipped for good), so it
// stays due; without a pause a broken SMTP relay would be retried every tick.
const digestRetryAfter = 30 * time.Minute

// tickDigest sends every due digest. retry maps a user id to the earliest time
// a failed send may be retried; it lives in memory only, so a restart retries
// immediately, which is harmless.
func tickDigest(ctx context.Context, st *store.Store, sender *digest.Sender, fallback store.SMTPSettings, lg *slog.Logger, now time.Time, retry map[int64]time.Time) {
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
	for _, d := range digests {
		if !dueDigest(d, now) {
			continue
		}
		if now.Before(retry[d.UserID]) {
			continue
		}
		u, err := st.GetUser(ctx, d.UserID)
		if err != nil {
			lg.Warn("digest: load user", "user_id", d.UserID, "err", err)
			continue
		}
		n, err := sender.SendForUser(ctx, u, d, now)
		if err != nil {
			// Not marked sent: the next attempt's window still starts at the
			// last successful send, so these articles go out once SMTP works.
			lg.Warn("digest: send failed", "user_id", d.UserID, "err", err, "retry_in", digestRetryAfter)
			retry[d.UserID] = now.Add(digestRetryAfter)
			continue
		}
		delete(retry, d.UserID)
		if n == 0 {
			lg.Debug("digest: no articles", "user_id", d.UserID)
			_ = st.MarkDigestSent(ctx, d.UserID, now.Unix())
			continue
		}
		lg.Info("digest sent", "user_id", d.UserID, "articles", n)
		_ = st.MarkDigestSent(ctx, d.UserID, now.Unix())
	}
}

// dueDigest is true when the most recent scheduled occurrence at or before now
// (today's UTC hour/minute, or yesterday's if today's is still ahead) has not
// been sent yet. Granularity is the 5-minute ticker, so the send fires within
// ~5 min of the configured hour/minute.
//
// Anchoring on the occurrence rather than the calendar day matters near
// midnight: a 23:59 schedule ticked at 23:57 and 00:02 must still fire on the
// 00:02 tick, and a process that was down across midnight still owes
// yesterday's send when it comes back.
func dueDigest(d models.UserDigest, now time.Time) bool {
	if !d.Enabled {
		return false
	}
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	scheduled := dayStart.Add(time.Duration(d.HourUTC)*time.Hour + time.Duration(d.MinuteUTC)*time.Minute)
	if now.Before(scheduled) {
		// Never sent: wait for the first scheduled time instead of firing
		// the moment the digest is enabled and then again at its hour.
		if d.LastSentAt == 0 {
			return false
		}
		scheduled = scheduled.AddDate(0, 0, -1)
	}
	return d.LastSentAt < scheduled.Unix()
}
