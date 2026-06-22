package api

import (
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/brandonhon/ember/internal/store"
)

// Default location for ad-hoc backups when the admin hasn't set a custom
// directory (the `db_backup_dir` app setting). To persist outside the
// container, point it at a bind-mounted host path — see docs/configuration.
const defaultBackupDir = "/data/backups"

// Default location for scheduled OPML exports (the `opml_export_dir` app
// setting); bind-mount it to persist outside the container.
const defaultExportDir = "/data/exports"

// dbStatus is the response for GET /api/admin/db. Reports size, current
// schedule settings, and on-disk backups.
type dbStatus struct {
	SizeBytes        int64              `json:"size_bytes"`
	PageCount        int64              `json:"page_count"`
	BackupDir        string             `json:"backup_dir"`
	Backups          []store.BackupInfo `json:"backups"`
	BackupSchedule   string             `json:"backup_schedule"`   // "off" | "daily" | "weekly"
	BackupKeepCount  int                `json:"backup_keep_count"` // how many to retain
	CleanupSchedule  string             `json:"cleanup_schedule"`  // "off" | "weekly" | "monthly"
	CleanupOlderDays int                `json:"cleanup_older_days"`
	OPMLSchedule     string             `json:"opml_schedule"` // "off" | "weekly" | "monthly"
	OPMLExportDir    string             `json:"opml_export_dir"`
	OPMLKeepCount    int                `json:"opml_keep"`
}

const (
	keyBackupSchedule   = "db_backup_schedule"
	keyBackupKeep       = "db_backup_keep"
	keyCleanupSchedule  = "db_cleanup_schedule"
	keyCleanupOlderDays = "db_cleanup_older_days"
	keyBackupDir        = "db_backup_dir"
	keyOPMLExportDir    = "opml_export_dir"
	keyOPMLKeep         = "opml_keep"
)

// validDirSetting reports whether p is acceptable as a backup/export directory:
// empty (reset to default) or an absolute path with no single quote (the
// store's VACUUM INTO path can't be parameterized).
func validDirSetting(p string) bool {
	return p == "" || (strings.HasPrefix(p, "/") && !strings.ContainsRune(p, '\''))
}

// resolveBackupDir returns the admin-configured backup directory, falling back
// to defaultBackupDir when unset.
func (d *Dependencies) resolveBackupDir(r *http.Request) string {
	return getSettingOr(r, d, keyBackupDir, defaultBackupDir)
}

func (d *Dependencies) handleGetDB(w http.ResponseWriter, r *http.Request) {
	size, pages, err := d.Store.DBSize(r.Context())
	if err != nil {
		internalError(w, "internal", err)
		return
	}
	dir := d.resolveBackupDir(r)
	backups, _ := d.Store.ListBackups(dir)
	resp := dbStatus{
		SizeBytes:        size,
		PageCount:        pages,
		BackupDir:        dir,
		Backups:          backups,
		BackupSchedule:   getSettingOr(r, d, keyBackupSchedule, "off"),
		BackupKeepCount:  getIntSettingOr(r, d, keyBackupKeep, 7),
		CleanupSchedule:  getSettingOr(r, d, keyCleanupSchedule, "off"),
		CleanupOlderDays: getIntSettingOr(r, d, keyCleanupOlderDays, 90),
		OPMLSchedule:     getSettingOr(r, d, "opml_schedule", "off"),
		OPMLExportDir:    getSettingOr(r, d, keyOPMLExportDir, defaultExportDir),
		OPMLKeepCount:    getIntSettingOr(r, d, keyOPMLKeep, 12),
	}
	writeData(w, http.StatusOK, resp, nil)
}

func (d *Dependencies) handleDBBackup(w http.ResponseWriter, r *http.Request) {
	info, err := d.Store.Backup(r.Context(), d.resolveBackupDir(r))
	if errors.Is(err, fs.ErrPermission) {
		// A bind-mounted host path that isn't writable by the container user is
		// the common failure — give the admin an actionable message, not a 500.
		writeError(w, http.StatusConflict, "backup_unwritable",
			"Backup failed: the backup directory isn't writable by the server. If it's a bind-mounted host path, make it owned by or writable by the container user (UID 65532) — see the docs.")
		return
	}
	if err != nil {
		internalError(w, "backup", err)
		return
	}
	writeData(w, http.StatusOK, info, nil)
}

type cleanupReq struct {
	OlderDays int `json:"older_days"`
}

func (d *Dependencies) handleDBCleanup(w http.ResponseWriter, r *http.Request) {
	var req cleanupReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.OlderDays <= 0 {
		req.OlderDays = 90
	}
	stats, err := d.Store.Cleanup(r.Context(), time.Duration(req.OlderDays)*24*time.Hour)
	if err != nil {
		internalError(w, "cleanup", err)
		return
	}
	writeData(w, http.StatusOK, stats, nil)
}

type scheduleReq struct {
	BackupSchedule   string `json:"backup_schedule"`
	BackupKeepCount  int    `json:"backup_keep_count"`
	BackupDir        string `json:"backup_dir"`
	CleanupSchedule  string `json:"cleanup_schedule"`
	CleanupOlderDays int    `json:"cleanup_older_days"`
	OPMLSchedule     string `json:"opml_schedule"`
	OPMLExportDir    string `json:"opml_export_dir"`
	OPMLKeepCount    int    `json:"opml_keep"`
}

func (d *Dependencies) handleDBSchedule(w http.ResponseWriter, r *http.Request) {
	var req scheduleReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if !validSchedule(req.BackupSchedule, "off", "daily", "weekly") {
		writeError(w, http.StatusBadRequest, "bad_request", "backup_schedule must be off|daily|weekly")
		return
	}
	if !validSchedule(req.CleanupSchedule, "off", "weekly", "monthly") {
		writeError(w, http.StatusBadRequest, "bad_request", "cleanup_schedule must be off|weekly|monthly")
		return
	}
	if req.OPMLSchedule != "" && !validSchedule(req.OPMLSchedule, "off", "weekly", "monthly") {
		writeError(w, http.StatusBadRequest, "bad_request", "opml_schedule must be off|weekly|monthly")
		return
	}
	if req.BackupKeepCount < 1 {
		req.BackupKeepCount = 7
	}
	if req.CleanupOlderDays < 7 {
		req.CleanupOlderDays = 7
	}
	if req.OPMLKeepCount < 1 {
		req.OPMLKeepCount = 12
	}
	// Backup/export directories: empty resets to the default; otherwise require
	// an absolute path with no single quote. The admin must bind-mount these
	// paths for the files to persist outside the container.
	backupDir := strings.TrimSpace(req.BackupDir)
	exportDir := strings.TrimSpace(req.OPMLExportDir)
	if !validDirSetting(backupDir) {
		writeError(w, http.StatusBadRequest, "bad_request", "backup_dir must be an absolute path with no quote characters")
		return
	}
	if !validDirSetting(exportDir) {
		writeError(w, http.StatusBadRequest, "bad_request", "opml_export_dir must be an absolute path with no quote characters")
		return
	}
	ctx := r.Context()
	if err := d.Store.PutAppSetting(ctx, keyBackupDir, backupDir); err != nil {
		internalError(w, "internal", err)
		return
	}
	if err := d.Store.PutAppSetting(ctx, keyOPMLExportDir, exportDir); err != nil {
		internalError(w, "internal", err)
		return
	}
	if err := d.Store.PutAppSetting(ctx, keyOPMLKeep, strconv.Itoa(req.OPMLKeepCount)); err != nil {
		internalError(w, "internal", err)
		return
	}
	if err := d.Store.PutAppSetting(ctx, keyBackupSchedule, req.BackupSchedule); err != nil {
		internalError(w, "internal", err)
		return
	}
	if err := d.Store.PutAppSetting(ctx, keyBackupKeep, strconv.Itoa(req.BackupKeepCount)); err != nil {
		internalError(w, "internal", err)
		return
	}
	if err := d.Store.PutAppSetting(ctx, keyCleanupSchedule, req.CleanupSchedule); err != nil {
		internalError(w, "internal", err)
		return
	}
	if err := d.Store.PutAppSetting(ctx, keyCleanupOlderDays, strconv.Itoa(req.CleanupOlderDays)); err != nil {
		internalError(w, "internal", err)
		return
	}
	if req.OPMLSchedule != "" {
		if err := d.Store.PutAppSetting(ctx, "opml_schedule", req.OPMLSchedule); err != nil {
			internalError(w, "internal", err)
			return
		}
	}
	writeData(w, http.StatusOK, map[string]string{"ok": "saved"}, nil)
}

func validSchedule(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

func getSettingOr(r *http.Request, d *Dependencies, key, fallback string) string {
	v, _ := d.Store.GetAppSetting(r.Context(), key)
	if v == "" {
		return fallback
	}
	return v
}
func getIntSettingOr(r *http.Request, d *Dependencies, key string, fallback int) int {
	v, _ := d.Store.GetAppSetting(r.Context(), key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
