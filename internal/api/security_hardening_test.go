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
