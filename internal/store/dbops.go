package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// BackupInfo describes a single on-disk backup.
type BackupInfo struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	CreatedAt int64  `json:"created_at"`
}

// Backup writes a consistent snapshot of the database to a timestamped file
// under dir. Uses SQLite's `VACUUM INTO` which produces a fully-compacted copy
// and works on a live database. Returns the new file's BackupInfo.
func (s *Store) Backup(ctx context.Context, dir string) (BackupInfo, error) {
	if dir == "" {
		return BackupInfo{}, fmt.Errorf("backup: empty directory")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return BackupInfo{}, fmt.Errorf("backup: mkdir %s: %w", dir, err)
	}
	name := time.Unix(s.nowUnix(), 0).UTC().Format("ember-2006-01-02-150405.db")
	out := filepath.Join(dir, name)
	// VACUUM INTO refuses to overwrite, so make sure we have a fresh path.
	if _, err := os.Stat(out); err == nil {
		return BackupInfo{}, fmt.Errorf("backup: %s already exists", out)
	}
	// VACUUM INTO can't bind a placeholder for the path, but we control the
	// timestamp filename so no injection risk.
	q := fmt.Sprintf("VACUUM INTO '%s'", strings.ReplaceAll(out, "'", "''"))
	if _, err := s.DB.ExecContext(ctx, q); err != nil {
		return BackupInfo{}, fmt.Errorf("backup: %w", err)
	}
	fi, err := os.Stat(out)
	if err != nil {
		return BackupInfo{}, fmt.Errorf("backup: stat %s: %w", out, err)
	}
	return BackupInfo{Path: out, SizeBytes: fi.Size(), CreatedAt: fi.ModTime().Unix()}, nil
}

// ListBackups returns the backups under dir, newest first.
func (s *Store) ListBackups(dir string) ([]BackupInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []BackupInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".db") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, BackupInfo{
			Path:      filepath.Join(dir, e.Name()),
			SizeBytes: fi.Size(),
			CreatedAt: fi.ModTime().Unix(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

// PruneBackups deletes backups older than the keepCount newest. Used by the
// scheduled-backup goroutine to avoid filling disk forever.
func (s *Store) PruneBackups(dir string, keep int) (int, error) {
	if keep <= 0 {
		return 0, nil
	}
	list, err := s.ListBackups(dir)
	if err != nil {
		return 0, err
	}
	if len(list) <= keep {
		return 0, nil
	}
	deleted := 0
	for _, b := range list[keep:] {
		if err := os.Remove(b.Path); err == nil {
			deleted++
		}
	}
	return deleted, nil
}

// ExportInfo describes a single on-disk OPML export.
type ExportInfo struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	CreatedAt int64  `json:"created_at"`
}

// ListExports returns the OPML exports under dir, newest first.
func (s *Store) ListExports(dir string) ([]ExportInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []ExportInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".opml") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, ExportInfo{
			Path:      filepath.Join(dir, e.Name()),
			SizeBytes: fi.Size(),
			CreatedAt: fi.ModTime().Unix(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

// PruneExports deletes exports older than the keepCount newest. Used by the
// scheduled-OPML-export goroutine to avoid filling disk forever.
func (s *Store) PruneExports(dir string, keep int) (int, error) {
	if keep <= 0 {
		return 0, nil
	}
	list, err := s.ListExports(dir)
	if err != nil {
		return 0, err
	}
	if len(list) <= keep {
		return 0, nil
	}
	deleted := 0
	for _, e := range list[keep:] {
		if err := os.Remove(e.Path); err == nil {
			deleted++
		}
	}
	return deleted, nil
}

// CleanupStats describes what a cleanup pass removed.
type CleanupStats struct {
	ArticlesDeleted int   `json:"articles_deleted"`
	BytesReclaimed  int64 `json:"bytes_reclaimed"`
}

// Cleanup removes articles older than `olderThan` (counting from
// published_at, falling back to fetched_at) that are NOT starred, in a board,
// or in a user's "read later" list. After deletion runs VACUUM to compact
// the file. Returns the count + bytes reclaimed.
func (s *Store) Cleanup(ctx context.Context, olderThan time.Duration) (CleanupStats, error) {
	cutoff := s.nowUnix() - int64(olderThan.Seconds())
	if cutoff <= 0 {
		return CleanupStats{}, fmt.Errorf("cleanup: olderThan must be > 0")
	}
	// Pre-cleanup size for the bytes-reclaimed calc.
	var pageCount, pageSize int64
	_ = s.DB.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pageCount)
	_ = s.DB.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&pageSize)
	before := pageCount * pageSize

	res, err := s.DB.ExecContext(ctx, `
		DELETE FROM articles
		WHERE IFNULL(published_at, fetched_at) < ?
		  AND id NOT IN (SELECT article_id FROM article_state WHERE is_starred = 1)
		  AND id NOT IN (SELECT article_id FROM article_state WHERE is_later = 1)
		  AND id NOT IN (SELECT article_id FROM board_articles)
		  AND id NOT IN (SELECT article_id FROM shares)
	`, cutoff)
	if err != nil {
		return CleanupStats{}, fmt.Errorf("cleanup delete: %w", err)
	}
	n, _ := res.RowsAffected()
	// Defragment the FTS5 index. Article deletes leave tombstones in the FTS
	// shadow tables; optimize merges segments so subsequent searches stay fast.
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO articles_fts(articles_fts) VALUES('optimize')`); err != nil {
		return CleanupStats{}, fmt.Errorf("cleanup fts optimize: %w", err)
	}
	// Compact the file to actually reclaim disk.
	if _, err := s.DB.ExecContext(ctx, `VACUUM`); err != nil {
		return CleanupStats{}, fmt.Errorf("cleanup vacuum: %w", err)
	}
	_ = s.DB.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pageCount)
	after := pageCount * pageSize
	return CleanupStats{ArticlesDeleted: int(n), BytesReclaimed: before - after}, nil
}

// DBSize returns the on-disk size + page count for the admin UI.
func (s *Store) DBSize(ctx context.Context) (int64, int64, error) {
	var pageCount, pageSize int64
	if err := s.DB.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pageCount); err != nil {
		return 0, 0, err
	}
	if err := s.DB.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&pageSize); err != nil {
		return 0, 0, err
	}
	return pageCount * pageSize, pageCount, nil
}
