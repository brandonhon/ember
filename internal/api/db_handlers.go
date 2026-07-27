package api

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/brandonhon/ember/internal/auth"
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
	Exports          []store.ExportInfo `json:"exports"`
}

const (
	keyBackupSchedule   = "db_backup_schedule"
	keyBackupKeep       = "db_backup_keep"
	keyCleanupSchedule  = "db_cleanup_schedule"
	keyCleanupOlderDays = "db_cleanup_older_days"
	keyBackupDir        = "db_backup_dir"
	keyOPMLExportDir    = "opml_export_dir"
	keyOPMLKeep         = "opml_keep"
	keyOPMLSchedule     = "opml_schedule"
)

// writeDirUnwritable reports a bind-mount permission failure as an actionable
// 409 rather than a generic 500. Deleting a file needs write permission on its
// containing directory too, so the create and delete paths share this message.
func writeDirUnwritable(w http.ResponseWriter, code, action, kind string) {
	writeError(w, http.StatusConflict, code, action+
		" failed: the "+kind+" directory isn't writable by the server. If it's a "+
		"bind-mounted host path, make it owned by or writable by the container "+
		"user (UID 65532) — see the docs.")
}

// validDirSetting reports whether p is acceptable as a backup/export directory:
// empty (reset to default) or an absolute path with no single quote (the
// store's VACUUM INTO path can't be parameterized).
func validDirSetting(p string) bool {
	return p == "" || (strings.HasPrefix(p, "/") && !strings.ContainsRune(p, '\''))
}

// resolveBackupDir returns the admin-configured backup directory, falling back
// to defaultBackupDir when unset.
func (d *Dependencies) resolveBackupDir(ctx context.Context) string {
	return d.settingOr(ctx, keyBackupDir, defaultBackupDir)
}

func (d *Dependencies) handleGetDB(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	size, pages, err := d.Store.DBSize(ctx)
	if err != nil {
		internalError(w, "internal", err)
		return
	}
	dir := d.resolveBackupDir(ctx)
	backups, _ := d.Store.ListBackups(dir)
	exportDir := d.settingOr(ctx, keyOPMLExportDir, defaultExportDir)
	exports, _ := d.Store.ListExports(exportDir)
	resp := dbStatus{
		SizeBytes:        size,
		PageCount:        pages,
		BackupDir:        dir,
		Backups:          backups,
		BackupSchedule:   d.settingOr(ctx, keyBackupSchedule, "off"),
		BackupKeepCount:  d.intSettingOr(ctx, keyBackupKeep, 7),
		CleanupSchedule:  d.settingOr(ctx, keyCleanupSchedule, "off"),
		CleanupOlderDays: d.intSettingOr(ctx, keyCleanupOlderDays, 90),
		OPMLSchedule:     d.settingOr(ctx, keyOPMLSchedule, "off"),
		OPMLExportDir:    exportDir,
		OPMLKeepCount:    d.intSettingOr(ctx, keyOPMLKeep, 12),
		Exports:          exports,
	}
	writeData(w, http.StatusOK, resp, nil)
}

func (d *Dependencies) handleDBBackup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	info, err := d.Store.Backup(ctx, d.resolveBackupDir(ctx))
	if errors.Is(err, fs.ErrPermission) {
		writeDirUnwritable(w, "backup_unwritable", "Backup", "backup")
		return
	}
	if err != nil {
		internalError(w, "backup", err)
		return
	}
	writeData(w, http.StatusOK, info, nil)
}

// handleOPMLExportNow writes the requesting admin's subscription list to the
// configured export directory, mirroring the manual DB "Back up now".
func (d *Dependencies) handleOPMLExportNow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, _ := auth.FromContext(ctx)
	dir := d.settingOr(ctx, keyOPMLExportDir, defaultExportDir)
	path, size, err := d.OPML.WriteExport(ctx, u.ID, dir)
	if errors.Is(err, fs.ErrPermission) {
		writeDirUnwritable(w, "export_unwritable", "Export", "export")
		return
	}
	if err != nil {
		internalError(w, "opml_export", err)
		return
	}
	_, _ = d.Store.PruneExports(dir, d.intSettingOr(ctx, keyOPMLKeep, 12))
	writeData(w, http.StatusOK, store.ExportInfo{Path: path, SizeBytes: size, CreatedAt: time.Now().Unix()}, nil)
}

// handleDeleteBackup removes a single backup file by name from the backup dir.
func (d *Dependencies) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	err := d.Store.DeleteBackup(d.resolveBackupDir(r.Context()), chi.URLParam(r, "name"))
	if errors.Is(err, fs.ErrPermission) {
		// A backup can have been created earlier, when the directory was still
		// writable, and only now fail to unlink.
		writeDirUnwritable(w, "backup_undeletable", "Delete", "backup")
		return
	}
	if mapStoreError(w, err) {
		return
	}
	writeOK(w)
}

// handleDeleteExport removes a single OPML export file by name from the export dir.
func (d *Dependencies) handleDeleteExport(w http.ResponseWriter, r *http.Request) {
	dir := d.settingOr(r.Context(), keyOPMLExportDir, defaultExportDir)
	err := d.Store.DeleteExport(dir, chi.URLParam(r, "name"))
	if errors.Is(err, fs.ErrPermission) {
		writeDirUnwritable(w, "export_undeletable", "Delete", "export")
		return
	}
	if mapStoreError(w, err) {
		return
	}
	writeOK(w)
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
	writes := []appSetting{
		{keyBackupDir, backupDir},
		{keyOPMLExportDir, exportDir},
		{keyOPMLKeep, strconv.Itoa(req.OPMLKeepCount)},
		{keyBackupSchedule, req.BackupSchedule},
		{keyBackupKeep, strconv.Itoa(req.BackupKeepCount)},
		{keyCleanupSchedule, req.CleanupSchedule},
		{keyCleanupOlderDays, strconv.Itoa(req.CleanupOlderDays)},
	}
	// An omitted opml_schedule leaves the stored value alone, unlike the fields
	// above which always write (their zero values were defaulted earlier).
	if req.OPMLSchedule != "" {
		writes = append(writes, appSetting{keyOPMLSchedule, req.OPMLSchedule})
	}
	if !d.putAppSettings(r.Context(), w, writes) {
		return
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

// appSetting is one key/value pair destined for the app_settings table.
type appSetting struct{ key, value string }

// putAppSettings writes each setting in order, reporting a 500 and returning
// false on the first failure. Not transactional — a mid-way failure leaves the
// earlier writes applied, same as the sequential writes it replaced.
func (d *Dependencies) putAppSettings(ctx context.Context, w http.ResponseWriter, settings []appSetting) bool {
	for _, s := range settings {
		if err := d.Store.PutAppSetting(ctx, s.key, s.value); err != nil {
			internalError(w, "internal", err)
			return false
		}
	}
	return true
}

// settingOr reads an app_settings value, falling back when it is unset.
func (d *Dependencies) settingOr(ctx context.Context, key, fallback string) string {
	v, _ := d.Store.GetAppSetting(ctx, key)
	if v == "" {
		return fallback
	}
	return v
}

// intSettingOr is settingOr for numeric settings; an unset or unparseable
// value yields the fallback.
func (d *Dependencies) intSettingOr(ctx context.Context, key string, fallback int) int {
	n, err := strconv.Atoi(d.settingOr(ctx, key, ""))
	if err != nil {
		return fallback
	}
	return n
}
