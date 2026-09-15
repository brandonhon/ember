package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/brandonhon/ember/internal/store"
)

// SummaryTimeoutSeconds bounds one summarization request (#201). Like the
// grace window, it is an admin setting overlaying an env default, so the
// round-trip and its bounds are what matter, plus that an out-of-range value
// is rejected rather than silently clamped.
func TestAdminSettings_SummaryTimeoutRoundTrip(t *testing.T) {
	h := newHarnessWith(t, func(d *Dependencies) { d.SummaryTimeoutSecondsFallback = 90 })
	h.seedUser(t, "admin", "correct-horse", true)
	cl := h.login(t, "admin", "correct-horse")
	url := h.srv.URL + "/api/admin/settings"

	get := func() map[string]any {
		t.Helper()
		resp, err := cl.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var env struct {
			Data map[string]any `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatal(err)
		}
		return env.Data
	}

	// GET reports the default plus its bounds.
	got := get()
	if n, _ := got["summary_timeout_seconds"].(float64); int(n) != 90 {
		t.Errorf("default = %v, want 90", got["summary_timeout_seconds"])
	}
	if n, _ := got["summary_timeout_seconds_floor"].(float64); int(n) != store.SummaryTimeoutSecondsFloor {
		t.Errorf("floor = %v, want %d", got["summary_timeout_seconds_floor"], store.SummaryTimeoutSecondsFloor)
	}
	if n, _ := got["summary_timeout_seconds_ceil"].(float64); int(n) != store.SummaryTimeoutSecondsCeil {
		t.Errorf("ceil = %v, want %d", got["summary_timeout_seconds_ceil"], store.SummaryTimeoutSecondsCeil)
	}

	// PATCH persists and echoes back.
	if code, body := patchJSON(t, cl, url, []byte(`{"summary_timeout_seconds":300}`)); code != http.StatusOK {
		t.Fatalf("patch 300 = %d: %s", code, body)
	}
	if n, _ := get()["summary_timeout_seconds"].(float64); int(n) != 300 {
		t.Errorf("after patch = %v, want 300", get()["summary_timeout_seconds"])
	}

	// Out-of-range is a 400 naming the field, not a silent clamp at the edge,
	// and the rejected write leaves the previous value alone.
	for _, bad := range []string{`{"summary_timeout_seconds":5}`, `{"summary_timeout_seconds":901}`} {
		if code, _ := patchJSON(t, cl, url, []byte(bad)); code != http.StatusBadRequest {
			t.Errorf("patch %s = %d, want 400", bad, code)
		}
	}
	if n, _ := get()["summary_timeout_seconds"].(float64); int(n) != 300 {
		t.Errorf("value after rejected writes = %v, want 300 unchanged", get()["summary_timeout_seconds"])
	}
}
