package api

import (
	"net/http"
	"strings"
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

// The OPML export directory + keep count mirror the DB backup ones: a custom
// absolute path and keep are saved and reflected; a bad path is rejected; empty
// resets to the default and keep < 1 clamps.
func TestDB_OPMLExportConfigurable(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "root", "hunter2", true)
	cl := h.login(t, "root", "hunter2")

	save := func(dir string, keep int) int {
		return post(t, cl, h.srv.URL+"/api/admin/db/schedule", map[string]any{
			"backup_schedule":    "off",
			"backup_keep_count":  7,
			"cleanup_schedule":   "off",
			"cleanup_older_days": 90,
			"opml_schedule":      "off",
			"opml_export_dir":    dir,
			"opml_keep":          keep,
		}, nil)
	}
	state := func() (string, int) {
		var st struct {
			Data struct {
				OPMLExportDir string `json:"opml_export_dir"`
				OPMLKeep      int    `json:"opml_keep"`
			} `json:"data"`
		}
		if code := get(t, cl, h.srv.URL+"/api/admin/db", &st); code != http.StatusOK {
			t.Fatalf("GET db = %d, want 200", code)
		}
		return st.Data.OPMLExportDir, st.Data.OPMLKeep
	}

	if dir, keep := state(); dir != "/data/exports" || keep != 12 {
		t.Fatalf("defaults = %q/%d, want /data/exports/12", dir, keep)
	}
	if code := save("/mnt/ember-exports", 5); code != http.StatusOK {
		t.Fatalf("save custom = %d, want 200", code)
	}
	if dir, keep := state(); dir != "/mnt/ember-exports" || keep != 5 {
		t.Fatalf("after save = %q/%d, want /mnt/ember-exports/5", dir, keep)
	}
	if code := save("relative", 5); code != http.StatusBadRequest {
		t.Fatalf("relative export dir = %d, want 400", code)
	}
	// Empty dir resets, keep < 1 clamps to 12.
	if code := save("", 0); code != http.StatusOK {
		t.Fatalf("reset = %d, want 200", code)
	}
	if dir, keep := state(); dir != "/data/exports" || keep != 12 {
		t.Fatalf("after reset = %q/%d, want /data/exports/12", dir, keep)
	}
}

// "Export now" writes an OPML file to the configured export directory and the
// status GET then lists it.
func TestDB_OPMLExportNow(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "root", "hunter2", true)
	cl := h.login(t, "root", "hunter2")

	tmp := t.TempDir() // absolute, writable
	if code := post(t, cl, h.srv.URL+"/api/admin/db/schedule", map[string]any{
		"backup_schedule":    "off",
		"backup_keep_count":  7,
		"cleanup_schedule":   "off",
		"cleanup_older_days": 90,
		"opml_schedule":      "off",
		"opml_export_dir":    tmp,
		"opml_keep":          12,
	}, nil); code != http.StatusOK {
		t.Fatalf("set export dir = %d, want 200", code)
	}
	if code := post(t, cl, h.srv.URL+"/api/admin/db/opml-export", map[string]any{}, nil); code != http.StatusOK {
		t.Fatalf("export now = %d, want 200", code)
	}
	var st struct {
		Data struct {
			Exports []struct {
				Path string `json:"path"`
			} `json:"exports"`
		} `json:"data"`
	}
	if code := get(t, cl, h.srv.URL+"/api/admin/db", &st); code != http.StatusOK {
		t.Fatalf("GET db = %d, want 200", code)
	}
	if len(st.Data.Exports) == 0 {
		t.Fatal("export list empty after Export now")
	}
}

// A backup and an OPML export can be deleted by name; a name that isn't a bare
// file of the right type is a 404 (no traversal, no touching other files).
func TestDB_DeleteBackupAndExport(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "root", "hunter2", true)
	cl := h.login(t, "root", "hunter2")

	tmp := t.TempDir()
	if code := post(t, cl, h.srv.URL+"/api/admin/db/schedule", map[string]any{
		"backup_schedule":    "off",
		"backup_keep_count":  7,
		"backup_dir":         tmp,
		"cleanup_schedule":   "off",
		"cleanup_older_days": 90,
		"opml_schedule":      "off",
		"opml_export_dir":    tmp,
		"opml_keep":          12,
	}, nil); code != http.StatusOK {
		t.Fatalf("set dirs = %d, want 200", code)
	}
	if code := post(t, cl, h.srv.URL+"/api/admin/db/backup", nil, nil); code != http.StatusOK {
		t.Fatalf("backup = %d, want 200", code)
	}
	if code := post(t, cl, h.srv.URL+"/api/admin/db/opml-export", map[string]any{}, nil); code != http.StatusOK {
		t.Fatalf("export = %d, want 200", code)
	}

	base := func(p string) string { parts := strings.Split(p, "/"); return parts[len(parts)-1] }
	var st struct {
		Data struct {
			Backups []struct {
				Path string `json:"path"`
			} `json:"backups"`
			Exports []struct {
				Path string `json:"path"`
			} `json:"exports"`
		} `json:"data"`
	}
	if code := get(t, cl, h.srv.URL+"/api/admin/db", &st); code != http.StatusOK || len(st.Data.Backups) == 0 || len(st.Data.Exports) == 0 {
		t.Fatalf("GET db = %d, backups=%d exports=%d", code, len(st.Data.Backups), len(st.Data.Exports))
	}

	// A non-.db / non-bare name is rejected as 404 (defense against traversal).
	if code := del(t, cl, h.srv.URL+"/api/admin/db/backups/evil.txt"); code != http.StatusNotFound {
		t.Fatalf("delete bad backup name = %d, want 404", code)
	}
	// Delete the real files.
	if code := del(t, cl, h.srv.URL+"/api/admin/db/backups/"+base(st.Data.Backups[0].Path)); code != http.StatusOK {
		t.Fatalf("delete backup = %d, want 200", code)
	}
	if code := del(t, cl, h.srv.URL+"/api/admin/db/exports/"+base(st.Data.Exports[0].Path)); code != http.StatusOK {
		t.Fatalf("delete export = %d, want 200", code)
	}
	var after struct {
		Data struct {
			Backups []any `json:"backups"`
			Exports []any `json:"exports"`
		} `json:"data"`
	}
	get(t, cl, h.srv.URL+"/api/admin/db", &after)
	if len(after.Data.Backups) != 0 || len(after.Data.Exports) != 0 {
		t.Fatalf("after delete: backups=%d exports=%d, want 0/0", len(after.Data.Backups), len(after.Data.Exports))
	}
}
