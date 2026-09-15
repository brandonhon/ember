package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// getAdminSettings reads the admin settings envelope as a typed struct.
func getAdminSettings(t *testing.T, cl *http.Client, base string) adminSettings {
	t.Helper()
	var env struct {
		Data adminSettings `json:"data"`
	}
	if code := get(t, cl, base+"/api/admin/settings", &env); code != http.StatusOK {
		t.Fatalf("GET /api/admin/settings = %d", code)
	}
	return env.Data
}

// patchAdminSettings PATCHes body and returns the echoed post-update view.
func patchAdminSettings(t *testing.T, cl *http.Client, base string, body []byte) adminSettings {
	t.Helper()
	code, raw := patchJSON(t, cl, base+"/api/admin/settings", body)
	if code != http.StatusOK {
		t.Fatalf("PATCH %s = %d: %s", body, code, raw)
	}
	var env struct {
		Data adminSettings `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode patch echo: %v (%s)", err, raw)
	}
	return env.Data
}

func smartCounts(t *testing.T, cl *http.Client, base string) store.SmartViewCounts {
	t.Helper()
	var env struct {
		Data store.SmartViewCounts `json:"data"`
	}
	if code := get(t, cl, base+"/api/me/smart-counts", &env); code != http.StatusOK {
		t.Fatalf("GET /api/me/smart-counts = %d", code)
	}
	return env.Data
}

func listArticleIDs(t *testing.T, cl *http.Client, base string) []int64 {
	t.Helper()
	var env struct {
		Data []models.Article `json:"data"`
	}
	if code := get(t, cl, base+"/api/articles", &env); code != http.StatusOK {
		t.Fatalf("GET /api/articles = %d", code)
	}
	ids := make([]int64, 0, len(env.Data))
	for _, a := range env.Data {
		ids = append(ids, a.ID)
	}
	return ids
}

// The whole of issue #198 in one round trip. Before this, "summaries on/off"
// was fixed at boot by EMBER_DISABLE_SUMMARIES, and the switch users found in
// the UI was a per-user display preference that never stopped summarization —
// so a reader with ~300 pending articles saw "Summarizing 314 articles…"
// forever, with those articles invisible behind the summary gate and nothing
// willing to finalize them. Turning the switch off must DRAIN that queue, not
// freeze it, and the choice must outlive a restart.
func TestAdminSettings_SummariesToggle_PersistsAndDrains(t *testing.T) {
	h := newHarnessWith(t, func(d *Dependencies) {
		wireOllama(d, "http://ollama.invalid", "llama3")
		// A long grace so freshly-ingested unsummarized articles really are
		// hidden by the gate — otherwise "the queue drained" and "the articles
		// became visible" would both pass for the wrong reason.
		d.SummaryGraceSecondsFallback = 3600
	})
	h.seedUser(t, "admin", "correct-horse", true)
	cl := h.login(t, "admin", "correct-horse")
	_, feedID := addFeedFor(t, h, cl, "https://toggle.test/feed")

	ctx := context.Background()
	now := time.Now().Unix()
	var pending []int64
	for i := 0; i < 3; i++ {
		guid := fmt.Sprintf("pending-%d", i)
		a, _, err := h.store.UpsertArticle(ctx, models.Article{
			FeedID: feedID, GUID: guid, Title: guid,
			ContentHash: "h-" + guid, PublishedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		pending = append(pending, a.ID)
	}
	summarized, _, err := h.store.UpsertArticle(ctx, models.Article{
		FeedID: feedID, GUID: "already-done", Title: "already-done",
		ContentHash: "h-done", PublishedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.UpdateSummary(ctx, summarized.ID, "a real summary", "llama3"); err != nil {
		t.Fatal(err)
	}

	// Precondition: summaries on, three articles queued and hidden.
	if got := getAdminSettings(t, cl, h.srv.URL); !got.SummariesEnabled {
		t.Fatal("precondition: summaries should start on (Ollama wired, no override)")
	}
	if n := smartCounts(t, cl, h.srv.URL).PendingSummary; n != 3 {
		t.Fatalf("precondition: pending_summary = %d, want 3", n)
	}
	if ids := listArticleIDs(t, cl, h.srv.URL); len(ids) != 1 {
		t.Fatalf("precondition: listed %v, want only the summarized article", ids)
	}

	// Off.
	got := patchAdminSettings(t, cl, h.srv.URL, []byte(`{"summaries_enabled":false}`))
	if got.SummariesEnabled {
		t.Fatal("patch echo should report summaries off")
	}
	if got := getAdminSettings(t, cl, h.srv.URL); got.SummariesEnabled {
		t.Fatal("GET should report summaries off after the patch")
	}

	// Requirement from #198: turning summaries off DRAINS the pending queue.
	if n := smartCounts(t, cl, h.srv.URL).PendingSummary; n != 0 {
		t.Fatalf("pending_summary = %d after disabling; the queue must drain, not freeze", n)
	}
	// ...because each pending row got a terminal marker. Assert the marker
	// directly: with the switch off the summary gate is bypassed anyway, so
	// "all 4 articles list" would hold whether or not the drain ran, and the
	// marker is the property actually under test.
	for _, id := range pending {
		art, err := h.store.GetArticle(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if art.SummaryModel != "disabled" {
			t.Errorf("article %d: summary_model = %q, want \"disabled\" — a NULL row stays queued forever", id, art.SummaryModel)
		}
	}
	// And they really are readable now rather than hidden behind the gate.
	if ids := listArticleIDs(t, cl, h.srv.URL); len(ids) != 4 {
		t.Fatalf("listed %v (%d articles), want all 4 visible once the queue drained", ids, len(ids))
	}

	// Turning it back on re-queues exactly the articles that were stamped
	// 'disabled' — not the one that had already succeeded.
	before := len(h.enqueued())
	got = patchAdminSettings(t, cl, h.srv.URL, []byte(`{"summaries_enabled":true}`))
	if !got.SummariesEnabled {
		t.Fatal("toggle did not come back on")
	}
	if n := smartCounts(t, cl, h.srv.URL).PendingSummary; n != 3 {
		t.Fatalf("pending_summary = %d after re-enabling, want 3", n)
	}
	requeued := map[int64]bool{}
	for _, id := range h.enqueued()[before:] {
		requeued[id] = true
	}
	for _, id := range pending {
		if !requeued[id] {
			t.Errorf("article %d was not re-enqueued after re-enabling (enqueued %v)", id, h.enqueued()[before:])
		}
	}
	if requeued[summarized.ID] {
		t.Errorf("article %d already had a summary but was re-enqueued — that burns inference for nothing", summarized.ID)
	}
	if art, err := h.store.GetArticle(ctx, summarized.ID); err != nil {
		t.Fatal(err)
	} else if art.SummaryModel != "llama3" {
		t.Errorf("finished summary was clobbered: summary_model = %q", art.SummaryModel)
	}
}

// The stored choice must beat the env-derived fallback, in both directions.
// This is the "restarting the container did not clear it" half of #198: the
// fallback is what a fresh process boots with, so if it won the article gate
// would silently flip back on every deploy.
func TestSummariesOn_StoredChoiceBeatsEnvFallback(t *testing.T) {
	ctx := context.Background()
	h := newHarnessWith(t, func(d *Dependencies) {
		wireOllama(d, "http://ollama.invalid", "llama3")
		d.SummariesEnabledFallback = true // EMBER_DISABLE_SUMMARIES unset
	})
	if !h.dep.summariesOn(ctx) {
		t.Fatal("precondition: fallback on with a backend wired should resolve on")
	}
	if err := h.store.PutSummariesEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	if h.dep.summariesOn(ctx) {
		t.Error("a stored off must beat an env fallback of on")
	}

	// The mirror image: EMBER_DISABLE_SUMMARIES=1 at boot, admin turns it on.
	off := newHarnessWith(t, func(d *Dependencies) {
		wireOllama(d, "http://ollama.invalid", "llama3")
		d.SummariesEnabledFallback = false
	})
	if off.dep.summariesOn(ctx) {
		t.Fatal("precondition: fallback off should resolve off")
	}
	if err := off.store.PutSummariesEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	if !off.dep.summariesOn(ctx) {
		t.Error("a stored on must beat an env fallback of off, or the toggle is un-turn-on-able")
	}

	// No backend at all: the setting cannot conjure one, so the gate stays off
	// and /api/me keeps telling the SPA to hide the Resummarize action.
	none := newHarnessWith(t, func(d *Dependencies) { d.SummariesEnabledFallback = true })
	if err := none.store.PutSummariesEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	if none.dep.summariesOn(ctx) {
		t.Error("summaries reported on with no summarizer configured")
	}
}

// PATCH /api/admin/settings is server-wide configuration; the drain it now
// performs rewrites every user's article rows. A non-admin reader must not be
// able to reach it.
func TestAdminSettings_SummariesToggle_IsAdminOnly(t *testing.T) {
	h := newHarnessWith(t, func(d *Dependencies) {
		wireOllama(d, "http://ollama.invalid", "llama3")
	})
	h.seedUser(t, "reader", "p", false)
	cl := h.login(t, "reader", "p")

	ctx := context.Background()
	_, feedID := addFeedFor(t, h, cl, "https://guard.test/feed")
	if _, _, err := h.store.UpsertArticle(ctx, models.Article{
		FeedID: feedID, GUID: "pending", Title: "pending",
		ContentHash: "h-pending", PublishedAt: time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}

	if code, _ := patchJSON(t, cl, h.srv.URL+"/api/admin/settings", []byte(`{"summaries_enabled":false}`)); code != http.StatusForbidden {
		t.Errorf("non-admin PATCH = %d, want 403", code)
	}
	// The rejected write must not have flipped anything...
	if !h.dep.summariesOn(ctx) {
		t.Error("a non-admin PATCH changed the global summaries switch")
	}
	// ...and, more damaging if it slipped through, must not have reached the
	// drain: that is an un-scoped UPDATE across every user's article rows.
	if n := smartCounts(t, cl, h.srv.URL).PendingSummary; n != 1 {
		t.Errorf("pending_summary = %d after a rejected PATCH, want 1 — the drain ran for a non-admin", n)
	}
}
