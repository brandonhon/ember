package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/brandonhon/ember/internal/feed"
)

// BackfillClusters populates canonical_url and cluster_id for articles
// that were inserted before the 0013_dedup_canonical migration. Walks
// rows in batches so a large historical corpus doesn't block startup.
//
// Idempotent: rows that already have a cluster_id are skipped. Safe to
// call on every boot — once the corpus is fully backfilled it's a
// no-op single SELECT.
//
// Returns the total number of rows updated.
func (s *Store) BackfillClusters(ctx context.Context, batchSize int) (int, error) {
	if batchSize <= 0 {
		batchSize = 500
	}
	total := 0
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		// Pick a batch of empty-cluster rows. We could ORDER BY id but
		// the cluster_id index is partial and the unscanned rows are
		// effectively random — order doesn't matter for correctness and
		// adding it would force a table scan for sort.
		rows, err := s.DB.QueryContext(ctx, `
			SELECT id, IFNULL(url,'')
			FROM articles
			WHERE cluster_id = '' AND IFNULL(url,'') <> ''
			LIMIT ?`, batchSize)
		if err != nil {
			return total, fmt.Errorf("backfill clusters: select: %w", err)
		}

		type row struct {
			id  int64
			url string
		}
		var batch []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.url); err != nil {
				rows.Close()
				return total, fmt.Errorf("backfill clusters: scan: %w", err)
			}
			batch = append(batch, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return total, fmt.Errorf("backfill clusters: iterate: %w", err)
		}
		if len(batch) == 0 {
			return total, nil
		}

		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return total, fmt.Errorf("backfill clusters: begin tx: %w", err)
		}
		stmt, err := tx.PrepareContext(ctx, `UPDATE articles SET canonical_url = ?, cluster_id = ? WHERE id = ?`)
		if err != nil {
			_ = tx.Rollback()
			return total, fmt.Errorf("backfill clusters: prepare: %w", err)
		}
		for _, r := range batch {
			canon := feed.CanonicalURL(r.url)
			cid := feed.ClusterID(canon)
			if cid == "" {
				// URL parsed to empty canonical — set cluster_id to a
				// stable marker so we don't pick this row up next pass.
				// Using the raw URL preserves uniqueness.
				cid = feed.ClusterID(r.url)
				canon = r.url
			}
			if _, err := stmt.ExecContext(ctx, canon, cid, r.id); err != nil {
				stmt.Close()
				_ = tx.Rollback()
				return total, fmt.Errorf("backfill clusters: update id=%d: %w", r.id, err)
			}
		}
		stmt.Close()
		if err := tx.Commit(); err != nil {
			return total, fmt.Errorf("backfill clusters: commit: %w", err)
		}
		total += len(batch)
		// If we got less than a full batch, we're done. Avoids one extra
		// empty SELECT at the end.
		if len(batch) < batchSize {
			return total, nil
		}
	}
}

// BackfillClustersAsync runs BackfillClusters in a goroutine and logs the
// outcome. Intended for boot wiring where we don't want to block server
// readiness on the migration. Uses the supplied logger and returns
// immediately. If ctx is cancelled, the in-progress batch finishes and
// the function exits cleanly (no further batches).
func (s *Store) BackfillClustersAsync(ctx context.Context, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	go func() {
		n, err := s.BackfillClusters(ctx, 0)
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			logger.Info("backfill clusters cancelled", "rows_done", n)
		case err != nil:
			logger.Error("backfill clusters failed", "error", err, "rows_done", n)
		case n > 0:
			logger.Info("backfill clusters complete", "rows", n)
		}
	}()
}
