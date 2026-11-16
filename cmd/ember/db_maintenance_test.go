package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/store"
)

// A job's *_last stamp must be the tick time that decided to run it, not the
// time it finished. dueSince compares the next tick's start against the stamp
// with >=; on an exactly-hourly ticker a stamp a few seconds after the tick
// leaves the 24th tick a few seconds short, so daily jobs ran every 25h and
// weekly ones every 169h.
func TestMaintenanceJobs_StampTickTime(t *testing.T) {
	ctx := context.Background()
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	// A tick time well in the past: any stamp taken from the wall clock after
	// the job finishes is distinguishable from it.
	tick := time.Now().Add(-time.Hour).Truncate(time.Second)

	jobs := []struct {
		key   string
		every time.Duration
		run   func(st *store.Store)
	}{
		{"retention_prune_last", 24 * time.Hour, func(st *store.Store) { runRetentionPrune(ctx, st, lg, tick) }},
		{"db_backup_last", 24 * time.Hour, func(st *store.Store) { runBackup(ctx, st, lg, tick) }},
		{"db_cleanup_last", 7 * 24 * time.Hour, func(st *store.Store) { runCleanup(ctx, st, lg, tick) }},
	}
	for _, j := range jobs {
		t.Run(j.key, func(t *testing.T) {
			st := store.NewTest(t)
			if err := st.PutAppSetting(ctx, "db_backup_dir", t.TempDir()); err != nil {
				t.Fatal(err)
			}
			j.run(st)
			if got := readIntSetting(ctx, st, j.key, 0); int64(got) != tick.Unix() {
				t.Fatalf("%s = %d, want the tick time %d", j.key, got, tick.Unix())
			}
			// Exactly one period of hourly ticks later the job is due again.
			if !dueSince(ctx, st, j.key, j.every, tick.Add(j.every)) {
				t.Errorf("%s not due again exactly %v after the tick that ran it", j.key, j.every)
			}
		})
	}
}
