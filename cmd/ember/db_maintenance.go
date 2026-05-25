package main

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/brandonhon/ember/internal/store"
)

const defaultBackupDir = "/data/backups"

// runDBMaintenance ticks every hour and runs the scheduled backup / cleanup
// actions when their app_setting cadence says they're due. Cadence values:
//   - backup_schedule:  off | daily | weekly
//   - cleanup_schedule: off | weekly | monthly
//
// Last-run timestamps are kept in app_settings under db_backup_last and
// db_cleanup_last so a restart doesn't trigger an immediate run.
func runDBMaintenance(ctx context.Context, st *store.Store, lg *slog.Logger) {
	t := time.NewTicker(1 * time.Hour)
	defer t.Stop()
	// First tick immediately so a restart catches up if something was missed.
	tickMaintenance(ctx, st, lg)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tickMaintenance(ctx, st, lg)
		}
	}
}

func tickMaintenance(ctx context.Context, st *store.Store, lg *slog.Logger) {
	now := time.Now()
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
