package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/summarize"
)

// The supported recovery path for issue #198: an admin who is staring at
// "Summarizing N articles…" that never goes down can drain the queue instead
// of hand-writing SQL against ember.db. Draining must both report a count and
// actually clear pending_summary — a handler that returns the right number
// while leaving the gate closed would look fixed and not be.
func TestDrainSummaryQueue_MakesPendingArticlesVisible(t *testing.T) {
	h := newHarnessWith(t, func(d *Dependencies) {
		d.Ollama = summarize.NewOllama("http://ollama.invalid", "llama3")
		// Long grace so the pending articles are genuinely hidden behind the
		// gate, not just fast-forwarded past it.
		d.SummaryGraceSecondsFallback = 3600
	})
	h.seedUser(t, "admin", "correct-horse", true)
	cl := h.login(t, "admin", "correct-horse")
	_, feedID := addFeedFor(t, h, cl, "https://drain.test/feed")

	ctx := context.Background()
	now := time.Now().Unix()
	for i := 0; i < 3; i++ {
		guid := fmt.Sprintf("pending-%d", i)
		if _, _, err := h.store.UpsertArticle(ctx, models.Article{
			FeedID: feedID, GUID: guid, Title: guid,
			ContentHash: "h-" + guid, PublishedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}

	if n := smartCounts(t, cl, h.srv.URL).PendingSummary; n != 3 {
		t.Fatalf("precondition: pending_summary = %d, want 3", n)
	}

	var out struct {
		Drained int64 `json:"drained"`
	}
	if code := postAdmin(t, cl, h.srv.URL, "/api/admin/summaries/drain", &out); code != http.StatusOK {
		t.Fatalf("POST /api/admin/summaries/drain = %d", code)
	}
	if out.Drained != 3 {
		t.Fatalf("drained %d, want 3", out.Drained)
	}
	if n := smartCounts(t, cl, h.srv.URL).PendingSummary; n != 0 {
		t.Fatalf("pending_summary = %d after drain, want 0", n)
	}
}

// A rejected admin call must not reach the bulk UPDATE. A 403 that still
// mutated the table would be strictly worse than no endpoint at all — a
// non-admin reader could drain every user's queue.
func TestDrainSummaryQueue_RequiresAdmin(t *testing.T) {
	h := newHarnessWith(t, func(d *Dependencies) {
		d.Ollama = summarize.NewOllama("http://ollama.invalid", "llama3")
	})
	h.seedUser(t, "reader", "p", false)
	cl := h.login(t, "reader", "p")
	_, feedID := addFeedFor(t, h, cl, "https://drain-guard.test/feed")

	ctx := context.Background()
	if _, _, err := h.store.UpsertArticle(ctx, models.Article{
		FeedID: feedID, GUID: "pending", Title: "pending",
		ContentHash: "h-pending", PublishedAt: time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}

	if code := postAdmin(t, cl, h.srv.URL, "/api/admin/summaries/drain", nil); code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", code)
	}
	if n := smartCounts(t, cl, h.srv.URL).PendingSummary; n != 1 {
		t.Errorf("pending_summary = %d after a rejected drain, want 1 — the drain ran for a non-admin", n)
	}
}

// Requeue is the inverse of drain, and it must stay narrow: only 'disabled'
// rows come back. 'skipped' (a real failure) and 'excluded' (a per-feed
// opt-out) are different states with their own recovery actions, and an
// already-summarized article must not be re-run at all.
func TestRequeueSummaries_ClearsOnlyDisabled(t *testing.T) {
	h := newHarnessWith(t, func(d *Dependencies) {
		d.Ollama = summarize.NewOllama("http://ollama.invalid", "llama3")
	})
	h.seedUser(t, "admin", "correct-horse", true)
	cl := h.login(t, "admin", "correct-horse")
	_, feedID := addFeedFor(t, h, cl, "https://requeue.test/feed")

	ctx := context.Background()
	now := time.Now().Unix()
	mk := func(guid string) models.Article {
		a, _, err := h.store.UpsertArticle(ctx, models.Article{
			FeedID: feedID, GUID: guid, Title: guid,
			ContentHash: "h-" + guid, PublishedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}

	disabled1 := mk("disabled-1")
	disabled2 := mk("disabled-2")
	skipped := mk("skipped")
	excluded := mk("excluded")
	summarized := mk("summarized")

	for _, id := range []int64{disabled1.ID, disabled2.ID} {
		if err := h.store.UpdateSummary(ctx, id, "", "disabled"); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.store.UpdateSummary(ctx, skipped.ID, "", "skipped"); err != nil {
		t.Fatal(err)
	}
	if err := h.store.UpdateSummary(ctx, excluded.ID, "", "excluded"); err != nil {
		t.Fatal(err)
	}
	if err := h.store.UpdateSummary(ctx, summarized.ID, "a real summary", "llama3"); err != nil {
		t.Fatal(err)
	}

	var out struct {
		Reset    int `json:"reset"`
		Enqueued int `json:"enqueued"`
	}
	if code := postAdmin(t, cl, h.srv.URL, "/api/admin/summaries/requeue", &out); code != http.StatusOK {
		t.Fatalf("POST /api/admin/summaries/requeue = %d", code)
	}
	if out.Reset != 2 {
		t.Fatalf("reset %d, want 2", out.Reset)
	}
	if out.Enqueued != 2 {
		t.Fatalf("enqueued %d, want 2", out.Enqueued)
	}

	// Assert the markers directly — a count matching is not proof the right
	// rows moved.
	get := func(id int64) string {
		a, err := h.store.GetArticle(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return a.SummaryModel
	}
	if m := get(disabled1.ID); m != "" {
		t.Errorf("disabled1 summary_model = %q, want cleared", m)
	}
	if m := get(disabled2.ID); m != "" {
		t.Errorf("disabled2 summary_model = %q, want cleared", m)
	}
	if m := get(skipped.ID); m != "skipped" {
		t.Errorf("skipped summary_model = %q, want untouched \"skipped\"", m)
	}
	if m := get(excluded.ID); m != "excluded" {
		t.Errorf("excluded summary_model = %q, want untouched \"excluded\"", m)
	}
	if m := get(summarized.ID); m != "llama3" {
		t.Errorf("summarized summary_model = %q, want untouched \"llama3\"", m)
	}
}

// Same guarantee as the drain guard, for the requeue side: a rejected call
// must leave the 'disabled' marker in place rather than sneaking the reset
// through before the 403 is returned.
func TestRequeueSummaries_RequiresAdmin(t *testing.T) {
	h := newHarnessWith(t, func(d *Dependencies) {
		d.Ollama = summarize.NewOllama("http://ollama.invalid", "llama3")
	})
	h.seedUser(t, "reader", "p", false)
	cl := h.login(t, "reader", "p")
	_, feedID := addFeedFor(t, h, cl, "https://requeue-guard.test/feed")

	ctx := context.Background()
	a, _, err := h.store.UpsertArticle(ctx, models.Article{
		FeedID: feedID, GUID: "disabled", Title: "disabled",
		ContentHash: "h-disabled", PublishedAt: time.Now().Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.UpdateSummary(ctx, a.ID, "", "disabled"); err != nil {
		t.Fatal(err)
	}

	if code := postAdmin(t, cl, h.srv.URL, "/api/admin/summaries/requeue", nil); code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", code)
	}
	art, err := h.store.GetArticle(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if art.SummaryModel != "disabled" {
		t.Errorf("summary_model = %q after a rejected requeue, want untouched \"disabled\" — the requeue ran for a non-admin", art.SummaryModel)
	}
}

// postAdmin POSTs an empty body to an admin action route and decodes the
// {"data": ...} envelope into dst (nil to ignore the body, e.g. on an
// expected 403).
func postAdmin(t *testing.T, c *http.Client, base, path string, dst any) int {
	t.Helper()
	if dst == nil {
		return post(t, c, base+path, nil, nil)
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	code := post(t, c, base+path, nil, &env)
	if len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, dst); err != nil {
			t.Fatalf("decode data envelope: %v (%s)", err, env.Data)
		}
	}
	return code
}
