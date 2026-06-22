package api

import (
	"net/http"
	"testing"
)

// The backup directory is an admin setting: a valid absolute path is saved and
// reflected by GET /api/admin/db; a relative path or one containing a quote is
// rejected; empty resets to the default.
func TestDB_BackupDirConfigurable(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "root", "hunter2", true)
	cl := h.login(t, "root", "hunter2")

	save := func(dir string) int {
		return post(t, cl, h.srv.URL+"/api/admin/db/schedule", map[string]any{
			"backup_schedule":    "off",
			"backup_keep_count":  7,
			"backup_dir":         dir,
			"cleanup_schedule":   "off",
			"cleanup_older_days": 90,
			"opml_schedule":      "off",
		}, nil)
	}
	dbDir := func() string {
		var st struct {
			Data struct {
				BackupDir string `json:"backup_dir"`
			} `json:"data"`
		}
		if code := get(t, cl, h.srv.URL+"/api/admin/db", &st); code != http.StatusOK {
			t.Fatalf("GET db = %d, want 200", code)
		}
		return st.Data.BackupDir
	}

	// Default before any setting.
	if got := dbDir(); got != "/data/backups" {
		t.Fatalf("default backup_dir = %q, want /data/backups", got)
	}
	// A custom absolute path is accepted and reflected.
	if code := save("/mnt/ember-backups"); code != http.StatusOK {
		t.Fatalf("save absolute dir = %d, want 200", code)
	}
	if got := dbDir(); got != "/mnt/ember-backups" {
		t.Fatalf("backup_dir = %q, want /mnt/ember-backups", got)
	}
	// Relative path and quoted path are rejected.
	if code := save("relative/path"); code != http.StatusBadRequest {
		t.Fatalf("relative dir = %d, want 400", code)
	}
	if code := save("/data/x'; DROP TABLE"); code != http.StatusBadRequest {
		t.Fatalf("quoted dir = %d, want 400", code)
	}
	// Empty resets to the default.
	if code := save(""); code != http.StatusOK {
		t.Fatalf("empty dir = %d, want 200", code)
	}
	if got := dbDir(); got != "/data/backups" {
		t.Fatalf("after reset backup_dir = %q, want /data/backups", got)
	}
}
