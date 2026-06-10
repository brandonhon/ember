package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/brandonhon/ember/internal/opml"
	"github.com/brandonhon/ember/internal/store"
)

const (
	defaultBackupDir = "/data/backups"
	defaultExportDir = "/data/exports"
)

// runDBMaintenance ticks every hour and runs the scheduled backup / cleanup
// actions when their app_setting cadence says they're due. Cadence values:
//   - backup_schedule:  off | daily | weekly
//   - cleanup_schedule: off | weekly | monthly
//
// Last-run timestamps are kept in app_settings under db_backup_last and
// db_cleanup_last so a restart doesn't trigger an immediate run.
func runDBMaintenance(ctx context.Context, st *store.Store, op *opml.Service, lg *slog.Logger) {
	t := time.NewTicker(1 * time.Hour)
	defer t.Stop()
	// First tick immediately so a restart catches up if something was missed.
	tickMaintenance(ctx, st, op, lg)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tickMaintenance(ctx, st, op, lg)
		}
	}
}

func tickMaintenance(ctx context.Context, st *store.Store, op *opml.Service, lg *slog.Logger) {
	now := time.Now()
	// Fixed retention: always-on, prunes articles older than the rolling
	// 1-week window (except starred / read-later / pinned / shared). Runs at
	// most once a day. This is the floor that backs the search-window cap —
	// you can't search what's been pruned. Distinct from the optional admin
	// "cleanup" below, which is operator-tunable and also VACUUMs.
	if dueSince(ctx, st, "retention_prune_last", 24*time.Hour, now) {
		runRetentionPrune(ctx, st, lg)
	}
	// Backups
	switch readSetting(ctx, st, "db_backup_schedule", "off") {
	case "daily":
		if dueSince(ctx, st, "db_backup_last", 24*time.Hour, now) {
			runBackup(ctx, st, lg)
		}
	case "weekly":
		if dueSince(ctx, st, "db_backup_last", 7*24*time.Hour, now) {
			runBackup(ctx, st, lg)
		}
	}
	// Cleanup
	switch readSetting(ctx, st, "db_cleanup_schedule", "off") {
	case "weekly":
		if dueSince(ctx, st, "db_cleanup_last", 7*24*time.Hour, now) {
			runCleanup(ctx, st, lg)
		}
	case "monthly":
		if dueSince(ctx, st, "db_cleanup_last", 30*24*time.Hour, now) {
			runCleanup(ctx, st, lg)
		}
	}
	// OPML export
	switch readSetting(ctx, st, "opml_schedule", "off") {
	case "weekly":
		if dueSince(ctx, st, "opml_last", 7*24*time.Hour, now) {
			runOPMLExport(ctx, st, op, lg)
		}
	case "monthly":
		if dueSince(ctx, st, "opml_last", 30*24*time.Hour, now) {
			runOPMLExport(ctx, st, op, lg)
		}
	}
}

// runOPMLExport writes the admin user's OPML to /data/exports/. We pick the
// first admin since OPML is per-user and a server-wide cron has no other
// natural choice. Multi-tenant deployments can disable this and trigger
// exports per-user via the manual endpoint instead.
func runOPMLExport(ctx context.Context, st *store.Store, op *opml.Service, lg *slog.Logger) {
	if op == nil {
		lg.Warn("opml export scheduled but service not initialized")
		return
	}
	adminID, err := st.FirstAdminID(ctx)
	if err != nil || adminID == 0 {
		lg.Warn("opml export: no admin user to export for", "err", err)
		return
	}
	if err := os.MkdirAll(defaultExportDir, 0o750); err != nil {
		lg.Warn("opml export: mkdir failed", "err", err)
		return
	}
	name := time.Now().UTC().Format("ember-2006-01-02-150405.opml")
	out := filepath.Join(defaultExportDir, name)
	f, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // G304: out is program-built from a fixed dir + timestamp, never user input.
	if err != nil {
		lg.Warn("opml export: create file", "err", err)
		return
	}
	defer func() {
		// Close error matters on a write file — a failed flush truncates
		// the export. Warn so a silently-corrupt backup is visible.
		if cerr := f.Close(); cerr != nil {
			lg.Warn("opml export: close file", "err", cerr)
		}
	}()
	if err := op.Export(ctx, adminID, f); err != nil {
		lg.Warn("opml export: write failed", "err", err)
		return
	}
	keep := readIntSetting(ctx, st, "opml_keep", 12)
	pruned, _ := st.PruneExports(defaultExportDir, keep)
	lg.Info("scheduled OPML export complete", "path", out, "user_id", adminID, "pruned", pruned)
	_ = st.PutAppSetting(ctx, "opml_last", strconv.FormatInt(time.Now().Unix(), 10))
}

func runBackup(ctx context.Context, st *store.Store, lg *slog.Logger) {
	info, err := st.Backup(ctx, defaultBackupDir)
	if err != nil {
		lg.Warn("scheduled backup failed", "err", err)
		return
	}
	keep := readIntSetting(ctx, st, "db_backup_keep", 7)
	pruned, _ := st.PruneBackups(defaultBackupDir, keep)
	lg.Info("scheduled backup complete", "path", info.Path, "size_bytes", info.SizeBytes, "pruned", pruned)
	_ = st.PutAppSetting(ctx, "db_backup_last", strconv.FormatInt(time.Now().Unix(), 10))
}

// runRetentionPrune removes articles past the fixed RetentionHours window.
// Delete-only (no VACUUM) so it's cheap enough to run daily; disk compaction
// is the optional admin Cleanup's job.
func runRetentionPrune(ctx context.Context, st *store.Store, lg *slog.Logger) {
	n, err := st.PruneArticles(ctx, time.Duration(store.RetentionHours)*time.Hour)
	if err != nil {
		lg.Warn("retention prune failed", "err", err)
		return
	}
	if n > 0 {
		lg.Info("retention prune complete", "articles_deleted", n, "retention_hours", store.RetentionHours)
	}
	_ = st.PutAppSetting(ctx, "retention_prune_last", strconv.FormatInt(time.Now().Unix(), 10))
}

func runCleanup(ctx context.Context, st *store.Store, lg *slog.Logger) {
	days := readIntSetting(ctx, st, "db_cleanup_older_days", 90)
	stats, err := st.Cleanup(ctx, time.Duration(days)*24*time.Hour)
	if err != nil {
		lg.Warn("scheduled cleanup failed", "err", err)
		return
	}
	lg.Info("scheduled cleanup complete", "articles_deleted", stats.ArticlesDeleted, "bytes_reclaimed", stats.BytesReclaimed)
	_ = st.PutAppSetting(ctx, "db_cleanup_last", strconv.FormatInt(time.Now().Unix(), 10))
}

func dueSince(ctx context.Context, st *store.Store, key string, every time.Duration, now time.Time) bool {
	last := readIntSetting(ctx, st, key, 0)
	if last == 0 {
		return true
	}
	return now.Sub(time.Unix(int64(last), 0)) >= every
}

func readSetting(ctx context.Context, st *store.Store, key, fallback string) string {
	v, _ := st.GetAppSetting(ctx, key)
	if v == "" {
		return fallback
	}
	return v
}

func readIntSetting(ctx context.Context, st *store.Store, key string, fallback int) int {
	v, _ := st.GetAppSetting(ctx, key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
