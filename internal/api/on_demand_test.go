package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/brandonhon/ember/internal/models"
)

// seedDeferredArticle adds a feed for the logged-in client, subscribes them
// (addFeedFor already does that), and inserts one article already stamped
// 'deferred' — standing in for what summarizeOne leaves behind in on-demand
// mode.
func seedDeferredArticle(t *testing.T, h *harness, c *http.Client, guid string) (feedID, articleID int64) {
	t.Helper()
	_, feedID = addFeedFor(t, h, c, fmt.Sprintf("https://ondemand.test/%s", guid))
	ctx := context.Background()
	art, _, err := h.store.UpsertArticle(ctx, models.Article{
		FeedID: feedID, GUID: guid, Title: guid,
		ContentText: "Body text long enough to summarize.", ContentHash: "h-" + guid,
		PublishedAt: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.UpdateSummary(ctx, art.ID, "", "deferred"); err != nil {
		t.Fatal(err)
	}
	return feedID, art.ID
}

// Starring a deferred article is the reader saying "I mean to read this" —
// the trigger that turns on-demand mode into an actual summary (issue #199).
func TestStar_RequestsASummaryForADeferredArticle(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "p", false)
	c := h.login(t, "alice", "p")
	_, articleID := seedDeferredArticle(t, h, c, "star-me")

	before := len(h.enqueued())
	if code := post(t, c, h.srv.URL+"/api/articles/star",
		map[string]any{"id": articleID, "value": true}, nil); code != http.StatusOK {
		t.Fatalf("star = %d", code)
	}

	got, err := h.store.GetArticle(context.Background(), articleID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummaryModel != "" {
		t.Fatalf("summary_model = %q; starring should have cleared 'deferred'", got.SummaryModel)
	}
	found := false
	for _, id := range h.enqueued()[before:] {
		if id == articleID {
			found = true
		}
	}
	if !found {
		t.Fatalf("article %d was not enqueued after starring (enqueued %v)", articleID, h.enqueued())
	}
}

// Un-starring is a removal signal, not "I mean to read this" — it must not
// spend inference the reader never asked for.
func TestUnstar_DoesNotRequestASummary(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "p", false)
	c := h.login(t, "alice", "p")
	_, articleID := seedDeferredArticle(t, h, c, "unstar-me")

	before := len(h.enqueued())
	if code := post(t, c, h.srv.URL+"/api/articles/star",
		map[string]any{"id": articleID, "value": false}, nil); code != http.StatusOK {
		t.Fatalf("unstar = %d", code)
	}

	got, err := h.store.GetArticle(context.Background(), articleID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummaryModel != "deferred" {
		t.Fatalf("summary_model = %q, want still \"deferred\" — un-starring must not request a summary", got.SummaryModel)
	}
	if n := len(h.enqueued()) - before; n != 0 {
		t.Fatalf("enqueued %d articles on un-star, want 0", n)
	}
}

// Pinning to a board is the third "I mean to read this" signal, gated by the
// per-board summarize flag: a board a user files things in rather than reads
// from must not spend inference just because an article landed there.
func TestBoardAdd_RespectsThePerBoardFlag(t *testing.T) {
	h := newHarness(t)
	u := h.seedUser(t, "alice", "p", false)
	c := h.login(t, "alice", "p")

	board, err := h.store.CreateBoard(context.Background(), models.Board{UserID: u.ID, Name: "Reading"})
	if err != nil {
		t.Fatal(err)
	}
	fileBoard, err := h.store.CreateBoard(context.Background(), models.Board{UserID: u.ID, Name: "Archive"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.UpdateBoard(context.Background(), u.ID, fileBoard.ID, false); err != nil {
		t.Fatal(err)
	}

	_, art1 := seedDeferredArticle(t, h, c, "pin-summarize")
	_, art2 := seedDeferredArticle(t, h, c, "pin-no-summarize")

	before := len(h.enqueued())
	if code := post(t, c, fmt.Sprintf("%s/api/boards/%d/articles", h.srv.URL, board.ID),
		map[string]any{"article_id": art1}, nil); code != http.StatusOK {
		t.Fatalf("pin to summarize=1 board = %d", code)
	}
	got1, err := h.store.GetArticle(context.Background(), art1)
	if err != nil {
		t.Fatal(err)
	}
	if got1.SummaryModel != "" {
		t.Fatalf("summary_model = %q; pinning to a summarize=1 board should have cleared 'deferred'", got1.SummaryModel)
	}
	found := false
	for _, id := range h.enqueued()[before:] {
		if id == art1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("article %d was not enqueued after pinning to a summarize=1 board", art1)
	}

	before = len(h.enqueued())
	if code := post(t, c, fmt.Sprintf("%s/api/boards/%d/articles", h.srv.URL, fileBoard.ID),
		map[string]any{"article_id": art2}, nil); code != http.StatusOK {
		t.Fatalf("pin to summarize=0 board = %d", code)
	}
	got2, err := h.store.GetArticle(context.Background(), art2)
	if err != nil {
		t.Fatal(err)
	}
	if got2.SummaryModel != "deferred" {
		t.Fatalf("summary_model = %q, want still \"deferred\" — a filing board must not request a summary", got2.SummaryModel)
	}
	if n := len(h.enqueued()) - before; n != 0 {
		t.Fatalf("enqueued %d articles pinning to a summarize=0 board, want 0", n)
	}
}

// BoardSummarizes is user-scoped like every other board lookup: a foreign
// board id must not leak whether it exists, let alone trigger inference on
// someone else's behalf.
func TestBoardAdd_ForeignBoardDoesNotTrigger(t *testing.T) {
	h := newHarness(t)
	owner := h.seedUser(t, "alice", "p", false)
	h.seedUser(t, "bob", "p", false)
	cBob := h.login(t, "bob", "p")

	board, err := h.store.CreateBoard(context.Background(), models.Board{UserID: owner.ID, Name: "Alice's board"})
	if err != nil {
		t.Fatal(err)
	}
	_, articleID := seedDeferredArticle(t, h, cBob, "foreign-board")

	before := len(h.enqueued())
	code, _ := postJSON(t, cBob, fmt.Sprintf("%s/api/boards/%d/articles", h.srv.URL, board.ID),
		[]byte(fmt.Sprintf(`{"article_id":%d}`, articleID)))
	if code == http.StatusOK {
		t.Fatalf("pinning to a foreign board succeeded (%d), want a failure status", code)
	}
	got, err := h.store.GetArticle(context.Background(), articleID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummaryModel != "deferred" {
		t.Fatalf("summary_model = %q, want still \"deferred\" — a rejected pin to a foreign board must not request a summary", got.SummaryModel)
	}
	if n := len(h.enqueued()) - before; n != 0 {
		t.Fatalf("enqueued %d articles on a rejected foreign-board pin, want 0", n)
	}
}
