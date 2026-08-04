package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/store"
	"github.com/brandonhon/ember/internal/summarize"
)

// The grace window bounds how long an article stays hidden waiting for its AI
// summary (#162). It is an admin setting overlaying an env default, so the
// round-trip and its bounds are what matter.
func TestAdminSettings_SummaryGraceRoundTrip(t *testing.T) {
	h := newHarnessWith(t, func(d *Dependencies) { d.SummaryGraceSecondsFallback = 120 })
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

	// Defaults come from the env-derived fallback, with the bounds echoed so
	// the UI can constrain its input.
	got := get()
	if n, _ := got["summary_grace_seconds"].(float64); int(n) != 120 {
		t.Errorf("default = %v, want the 120 fallback", got["summary_grace_seconds"])
	}
	if n, _ := got["summary_grace_seconds_ceil"].(float64); int(n) != store.SummaryGraceSecondsCeil {
		t.Errorf("ceil = %v, want %d", got["summary_grace_seconds_ceil"], store.SummaryGraceSecondsCeil)
	}

	// 0 is a real value — "show articles as soon as they're fetched" — and must
	// not be coerced back to the default by an is-it-set style check.
	if code, body := patchJSON(t, cl, url, []byte(`{"summary_grace_seconds":0}`)); code != http.StatusOK {
		t.Fatalf("patch 0 = %d: %s", code, body)
	}
	if n, _ := get()["summary_grace_seconds"].(float64); int(n) != 0 {
		t.Errorf("after setting 0 the value read back as %v — zero was swallowed", get()["summary_grace_seconds"])
	}

	if code, body := patchJSON(t, cl, url, []byte(`{"summary_grace_seconds":45}`)); code != http.StatusOK {
		t.Fatalf("patch 45 = %d: %s", code, body)
	}
	if n, _ := get()["summary_grace_seconds"].(float64); int(n) != 45 {
		t.Errorf("value = %v, want 45", get()["summary_grace_seconds"])
	}

	// Out of range is rejected rather than silently clamped, so a typo in the
	// admin UI is visible instead of quietly changing behaviour.
	for _, bad := range []string{`{"summary_grace_seconds":-1}`, `{"summary_grace_seconds":99999}`} {
		if code, _ := patchJSON(t, cl, url, []byte(bad)); code != http.StatusBadRequest {
			t.Errorf("patch %s = %d, want 400", bad, code)
		}
	}
	// ...and the rejected write left the previous value alone.
	if n, _ := get()["summary_grace_seconds"].(float64); int(n) != 45 {
		t.Errorf("value after rejected writes = %v, want 45 unchanged", get()["summary_grace_seconds"])
	}
}

// The round-trip above covers how the setting is STORED. This covers how it is
// TRANSLATED into the cutoff timestamp, which is the value the article list,
// the smart counts and the per-feed unread query must all receive identically
// — the place where a wrong number desyncs a badge from the column it counts.
func TestSummaryGraceBefore_TranslatesSettingToCutoff(t *testing.T) {
	ctx := context.Background()
	withSummarizer := func(d *Dependencies) {
		d.Ollama = summarize.NewOllama("http://ollama.invalid", "llama3")
	}

	// No summarizer wired up: the gate is inactive everywhere, signalled by 0.
	off := newHarnessWith(t, func(d *Dependencies) { d.SummaryGraceSecondsFallback = 120 })
	if got := off.dep.summaryGraceBefore(ctx); got != 0 {
		t.Errorf("summaries off: cutoff = %d, want 0 (gate inactive)", got)
	}

	// A grace of 0 means "show articles as soon as they're fetched", which needs
	// a cutoff in the FUTURE: a cutoff of exactly now would still withhold an
	// article fetched during this same second.
	zero := newHarnessWith(t, func(d *Dependencies) {
		withSummarizer(d)
		d.SummaryGraceSecondsFallback = 0
	})
	if now, got := time.Now().Unix(), zero.dep.summaryGraceBefore(ctx); got <= now {
		t.Errorf("grace 0: cutoff = %d, want > now (%d) so a just-fetched article passes", got, now)
	}

	// A positive grace puts the cutoff that many seconds in the past.
	on := newHarnessWith(t, func(d *Dependencies) {
		withSummarizer(d)
		d.SummaryGraceSecondsFallback = 300
	})
	now := time.Now().Unix()
	if got := on.dep.summaryGraceBefore(ctx); now-got < 295 || now-got > 305 {
		t.Errorf("grace 300: cutoff is %ds before now, want ~300", now-got)
	}
}

func patchJSON(t *testing.T, cl *http.Client, url string, body []byte) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	echoCSRF(cl, url, req)
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}
