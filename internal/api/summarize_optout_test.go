package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/brandonhon/ember/internal/models"
)

// addFeedFor adds a feed for the logged-in client and returns the subscription
// and feed ids.
func addFeedFor(t *testing.T, h *harness, c *http.Client, url string) (subID, feedID int64) {
	t.Helper()
	var add struct {
		Data struct {
			Feed         models.Feed         `json:"feed"`
			Subscription models.Subscription `json:"subscription"`
		} `json:"data"`
	}
	if code := post(t, c, h.srv.URL+"/api/feeds", map[string]string{"url": url}, &add); code != http.StatusCreated {
		t.Fatalf("add feed %s = %d", url, code)
	}
	return add.Data.Subscription.ID, add.Data.Feed.ID
}

func (h *harness) enqueued() []int64 {
	h.noopPoller.mu.Lock()
	defer h.noopPoller.mu.Unlock()
	return append([]int64(nil), h.noopPoller.enqueuedIDs...)
}

// The PATCH round-trips through GET /api/feeds, which is what the SPA renders
// the menu label from. A flag the server stores but never reports back would
// leave the menu stuck on "Don't summarize".
func TestFeeds_PatchSummarize_RoundTrips(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "p", false)
	cA := h.login(t, "alice", "p")
	subID, _ := addFeedFor(t, h, cA, "https://opt.test/feed")

	var list struct {
		Data []models.FeedWithCounts `json:"data"`
	}
	get(t, cA, h.srv.URL+"/api/feeds", &list)
	if len(list.Data) != 1 || !list.Data[0].Summarize {
		t.Fatalf("precondition: new subscriptions must default to summarize=true, got %+v", list.Data)
	}

	if code := patch(t, cA, fmt.Sprintf("%s/api/feeds/%d", h.srv.URL, subID),
		map[string]any{"summarize": false}, nil); code != http.StatusOK {
		t.Fatalf("patch = %d", code)
	}
	get(t, cA, h.srv.URL+"/api/feeds", &list)
	if len(list.Data) != 1 || list.Data[0].Summarize {
		t.Errorf("after opt-out: summarize = %v, want false", list.Data[0].Summarize)
	}
}

// Opting back IN has to recover the articles stamped 'excluded' while off,
// otherwise the toggle looks inert until the feed publishes something new.
func TestFeeds_PatchSummarize_ReEnqueuesExcluded(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "p", false)
	cA := h.login(t, "alice", "p")
	subID, feedID := addFeedFor(t, h, cA, "https://backfill.test/feed")

	ctx := context.Background()
	off := false
	if code := patch(t, cA, fmt.Sprintf("%s/api/feeds/%d", h.srv.URL, subID),
		map[string]any{"summarize": off}, nil); code != http.StatusOK {
		t.Fatalf("opt out = %d", code)
	}
	// Stand in for the poller having skipped this article while opted out.
	art, _, err := h.store.UpsertArticle(ctx, models.Article{
		FeedID: feedID, GUID: "skipped-while-off", Title: "Skipped While Off",
		ContentHash: "h-off", PublishedAt: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.UpdateSummary(ctx, art.ID, "", "excluded"); err != nil {
		t.Fatal(err)
	}

	before := len(h.enqueued())
	if code := patch(t, cA, fmt.Sprintf("%s/api/feeds/%d", h.srv.URL, subID),
		map[string]any{"summarize": true}, nil); code != http.StatusOK {
		t.Fatalf("opt in = %d", code)
	}

	var found bool
	for _, id := range h.enqueued()[before:] {
		if id == art.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("opting back in did not re-enqueue the excluded article %d (enqueued %v)", art.ID, h.enqueued())
	}
	got, err := h.store.GetArticle(ctx, art.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummaryModel != "" {
		t.Errorf("excluded marker survived the opt-in: summary_model = %q", got.SummaryModel)
	}
}

// Opting out is per-subscription, so it must be reachable only through the
// caller's own subscription id, and must not touch anyone else's row.
func TestFeeds_PatchSummarize_IsPerUser(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "p", false)
	h.seedUser(t, "bob", "p", false)
	cA := h.login(t, "alice", "p")
	cB := h.login(t, "bob", "p")

	const url = "https://shared.test/feed"
	aliceSub, _ := addFeedFor(t, h, cA, url)
	addFeedFor(t, h, cB, url) // same shared feed row

	if code := patch(t, cB, fmt.Sprintf("%s/api/feeds/%d", h.srv.URL, aliceSub),
		map[string]any{"summarize": false}, nil); code != http.StatusNotFound {
		t.Errorf("bob patching alice's subscription = %d, want 404", code)
	}

	if code := patch(t, cA, fmt.Sprintf("%s/api/feeds/%d", h.srv.URL, aliceSub),
		map[string]any{"summarize": false}, nil); code != http.StatusOK {
		t.Fatalf("alice opt out = %d", code)
	}
	var list struct {
		Data []models.FeedWithCounts `json:"data"`
	}
	get(t, cB, h.srv.URL+"/api/feeds", &list)
	if len(list.Data) != 1 || !list.Data[0].Summarize {
		t.Errorf("alice's opt-out leaked to bob: %+v", list.Data)
	}
}
