package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/brandonhon/ember/internal/store"
)

// Default location for ad-hoc backups. Override via the EMBER_BACKUP_DIR env
// var (read in main.go) if running outside Docker.
const defaultBackupDir = "/data/backups"

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
}

const (
	keyBackupSchedule   = "db_backup_schedule"
	keyBackupKeep       = "db_backup_keep"
	keyCleanupSchedule  = "db_cleanup_schedule"
	keyCleanupOlderDays = "db_cleanup_older_days"
)

func (d *Dependencies) handleGetDB(w http.ResponseWriter, r *http.Request) {
	size, pages, err := d.Store.DBSize(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	backups, _ := d.Store.ListBackups(defaultBackupDir)
	resp := dbStatus{
		SizeBytes:        size,
		PageCount:        pages,
		BackupDir:        defaultBackupDir,
		Backups:          backups,
		BackupSchedule:   getSettingOr(r, d, keyBackupSchedule, "off"),
		BackupKeepCount:  getIntSettingOr(r, d, keyBackupKeep, 7),
		CleanupSchedule:  getSettingOr(r, d, keyCleanupSchedule, "off"),
		CleanupOlderDays: getIntSettingOr(r, d, keyCleanupOlderDays, 90),
	}
	writeData(w, http.StatusOK, resp, nil)
}

func (d *Dependencies) handleDBBackup(w http.ResponseWriter, r *http.Request) {
	info, err := d.Store.Backup(r.Context(), defaultBackupDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "backup_failed", err.Error())
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
		writeError(w, http.StatusInternalServerError, "cleanup_failed", err.Error())
		return
	}
	writeData(w, http.StatusOK, stats, nil)
}

type scheduleReq struct {
	BackupSchedule   string `json:"backup_schedule"`
	BackupKeepCount  int    `json:"backup_keep_count"`
	CleanupSchedule  string `json:"cleanup_schedule"`
	CleanupOlderDays int    `json:"cleanup_older_days"`
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
	if req.BackupKeepCount < 1 {
		req.BackupKeepCount = 7
	}
	if req.CleanupOlderDays < 7 {
		req.CleanupOlderDays = 7
	}
	ctx := r.Context()
	if err := d.Store.PutAppSetting(ctx, keyBackupSchedule, req.BackupSchedule); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if err := d.Store.PutAppSetting(ctx, keyBackupKeep, strconv.Itoa(req.BackupKeepCount)); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if err := d.Store.PutAppSetting(ctx, keyCleanupSchedule, req.CleanupSchedule); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if err := d.Store.PutAppSetting(ctx, keyCleanupOlderDays, strconv.Itoa(req.CleanupOlderDays)); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
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
