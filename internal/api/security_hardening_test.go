package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Search query is parameterized (not SQL injection) but must be length-capped
// so a caller can't hand SQLite an arbitrarily large FTS5 expression.
func TestSearch_RejectsOverlongQuery(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "hunter2", false)
	cl := h.login(t, "alice", "hunter2")

	long := strings.Repeat("a", maxSearchQueryLen+1)
	if code := get(t, cl, h.srv.URL+"/api/search?q="+long, nil); code != http.StatusBadRequest {
		t.Fatalf("overlong search = %d, want 400", code)
	}
	// A normal query is still accepted (no false positive from the cap).
	if code := get(t, cl, h.srv.URL+"/api/search?q=foo", nil); code == http.StatusBadRequest {
		t.Fatalf("normal search = 400, want it accepted")
	}
}

// The login endpoint returns an allowlisted view of the user, not the raw
// models.User — so a future sensitive field can't silently leak. In particular
// settings_json must not appear in the login body (the SPA re-pulls /api/me).
func TestLogin_ResponseIsAllowlisted(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "hunter2", false)
	jar, _ := newJar()
	cl := h.newClient(jar)

	var resp struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	code := post(t, cl, h.srv.URL+"/api/auth/login",
		map[string]string{"username": "alice", "password": "hunter2"}, &resp)
	if code != http.StatusOK {
		t.Fatalf("login = %d, want 200", code)
	}
	if _, leaked := resp.Data["settings_json"]; leaked {
		t.Fatal("login response includes settings_json; want allowlisted fields only")
	}
	for _, k := range []string{"id", "username", "is_admin"} {
		if _, ok := resp.Data[k]; !ok {
			t.Fatalf("login response missing required field %q", k)
		}
	}
}

// Filter-validation errors should carry the useful detail (which only echoes
// the user's own rule) but not the internal "filters:" package prefix.
func TestFilter_ValidationErrorOmitsInternalPrefix(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "hunter2", false)
	cl := h.login(t, "alice", "hunter2")

	var resp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	code := post(t, cl, h.srv.URL+"/api/filters", map[string]any{
		"name":       "bad",
		"match_json": `{"field":"bogus","op":"contains","value":"y"}`,
		"action":     "mark_read",
	}, &resp)
	if code != http.StatusBadRequest {
		t.Fatalf("create filter with bad field = %d, want 400", code)
	}
	if strings.Contains(resp.Error.Message, "filters:") {
		t.Fatalf("error message leaks the internal package prefix: %q", resp.Error.Message)
	}
	if !strings.Contains(resp.Error.Message, "bogus") {
		t.Fatalf("error message dropped the useful validation detail: %q", resp.Error.Message)
	}
}

// The branding favicon URL is rendered as a <link rel="icon"> href, so it must
// reject active-content schemes (javascript:/data:) and accept only a
// same-origin path or an https URL.
func TestBranding_RejectsUnsafeFaviconScheme(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "root", "hunter2", true)
	cl := h.login(t, "root", "hunter2")

	if code := post(t, cl, h.srv.URL+"/api/admin/branding",
		map[string]any{"favicon_url": "javascript:alert(1)"}, nil); code != http.StatusBadRequest {
		t.Fatalf("javascript: favicon = %d, want 400", code)
	}
	if code := post(t, cl, h.srv.URL+"/api/admin/branding",
		map[string]any{"favicon_url": "/icon.svg"}, nil); code != http.StatusOK {
		t.Fatalf("/path favicon = %d, want 200", code)
	}
	if code := post(t, cl, h.srv.URL+"/api/admin/branding",
		map[string]any{"favicon_url": "https://example.com/i.png"}, nil); code != http.StatusOK {
		t.Fatalf("https favicon = %d, want 200", code)
	}
}

// mark-all-read scoped to an unknown/foreign board or category id is a 404, not
// a silent count:0 — while an unscoped "mark everything" still succeeds.
func TestMarkAllRead_UnknownScopeReturns404(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "hunter2", false)
	cl := h.login(t, "alice", "hunter2")

	if code := post(t, cl, h.srv.URL+"/api/articles/mark-all-read",
		map[string]any{"board_id": 999999}, nil); code != http.StatusNotFound {
		t.Fatalf("unknown board_id = %d, want 404", code)
	}
	if code := post(t, cl, h.srv.URL+"/api/articles/mark-all-read",
		map[string]any{"category_id": 999999}, nil); code != http.StatusNotFound {
		t.Fatalf("unknown category_id = %d, want 404", code)
	}
	if code := post(t, cl, h.srv.URL+"/api/articles/mark-all-read",
		map[string]any{}, nil); code != http.StatusOK {
		t.Fatalf("unscoped mark-all-read = %d, want 200", code)
	}
}
