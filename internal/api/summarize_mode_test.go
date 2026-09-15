package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// feedFor reads the caller's single subscription back through GET /api/feeds —
// the endpoint the sidebar renders its menu from. A mode the server stores but
// never reports back would leave the three-way choice showing the wrong tick.
func feedFor(t *testing.T, c *http.Client, base string) models.FeedWithCounts {
	t.Helper()
	var list struct {
		Data []models.FeedWithCounts `json:"data"`
	}
	if code := get(t, c, base+"/api/feeds", &list); code != http.StatusOK {
		t.Fatalf("GET /api/feeds = %d", code)
	}
	if len(list.Data) != 1 {
		t.Fatalf("want exactly one feed, got %d", len(list.Data))
	}
	return list.Data[0]
}

// The server-wide mode has to survive the round trip through the settings
// endpoint the Settings UI reads: it is the fallback every feed without an
// override resolves against, so a value that writes but never reads back would
// leave the two-way choice permanently showing "Every article".
func TestAdminSettings_SummarizeMode_RoundTrips(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "admin", "p", true)
	cl := h.login(t, "admin", "p")

	// Default: every install behaved this way before the setting existed.
	if got := getAdminSettings(t, cl, h.srv.URL).SummarizeMode; got != store.ModeAll {
		t.Fatalf("default summarize_mode = %q, want %q", got, store.ModeAll)
	}

	echo := patchAdminSettings(t, cl, h.srv.URL, []byte(`{"summarize_mode":"on_demand"}`))
	if echo.SummarizeMode != store.ModeOnDemand {
		t.Errorf("patch echo = %q, want on_demand", echo.SummarizeMode)
	}
	if got := getAdminSettings(t, cl, h.srv.URL).SummarizeMode; got != store.ModeOnDemand {
		t.Errorf("re-read = %q, want on_demand", got)
	}
	// And the value the resolution path actually consults agrees with the DTO.
	if got := h.dep.globalSummarizeMode(context.Background()); got != store.ModeOnDemand {
		t.Errorf("globalSummarizeMode = %q, want on_demand", got)
	}

	if echo := patchAdminSettings(t, cl, h.srv.URL, []byte(`{"summarize_mode":"all"}`)); echo.SummarizeMode != store.ModeAll {
		t.Errorf("switching back = %q, want all", echo.SummarizeMode)
	}
}

// An unrecognized mode has to be refused with a 400 that names the field, and
// — the part that matters — must leave the stored mode alone. A validation
// gap here writes a value ResolveSummarizeMode then has to treat as corrupt.
func TestAdminSettings_SummarizeMode_RejectsInvalid(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "admin", "p", true)
	cl := h.login(t, "admin", "p")

	patchAdminSettings(t, cl, h.srv.URL, []byte(`{"summarize_mode":"on_demand"}`))

	for _, bad := range []string{`""`, `"sometimes"`, `"ALL"`} {
		body := []byte(`{"summarize_mode":` + bad + `}`)
		code, raw := patchJSON(t, cl, h.srv.URL+"/api/admin/settings", body)
		if code != http.StatusBadRequest {
			t.Errorf("PATCH %s = %d, want 400", body, code)
			continue
		}
		var env struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &env)
		if !strings.Contains(env.Error.Message, "summarize_mode") {
			t.Errorf("400 message %q does not name the field", env.Error.Message)
		}
	}
	// Nothing was persisted by any of the rejected writes.
	if got := getAdminSettings(t, cl, h.srv.URL).SummarizeMode; got != store.ModeOnDemand {
		t.Errorf("a rejected write changed the stored mode to %q", got)
	}
}

// The mode is a server-wide setting, so it lives behind the admin gate like
// every other one. The assertion that matters is the second: the rejection
// must not have written anything on the way to the 403.
func TestAdminSettings_SummarizeMode_AdminOnly(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "admin", "p", true)
	h.seedUser(t, "reader", "p", false)
	admin := h.login(t, "admin", "p")
	reader := h.login(t, "reader", "p")

	if code, _ := patchJSON(t, reader, h.srv.URL+"/api/admin/settings",
		[]byte(`{"summarize_mode":"on_demand"}`)); code != http.StatusForbidden {
		t.Errorf("non-admin PATCH = %d, want 403", code)
	}
	if got := getAdminSettings(t, admin, h.srv.URL).SummarizeMode; got != store.ModeAll {
		t.Errorf("a non-admin PATCH changed the server-wide mode to %q", got)
	}
}

// Switching the server-wide mode back to "every article" has to recover the
// backlog on-demand mode deferred, exactly as re-enabling summaries recovers
// what was stamped 'disabled'. Without it an admin flips the setting and
// watches nothing happen to the articles already on disk — the same defect the
// per-feed opt-out had to fix once, one level up.
func TestAdminSettings_SummarizeMode_BackfillsDeferred(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "admin", "p", true)
	cl := h.login(t, "admin", "p")

	ctx := context.Background()
	if err := h.store.PutSummarizeMode(ctx, store.ModeOnDemand); err != nil {
		t.Fatal(err)
	}
	_, articleID := seedDeferredArticle(t, h, cl, "global-backfill")

	before := len(h.enqueued())
	patchAdminSettings(t, cl, h.srv.URL, []byte(`{"summarize_mode":"all"}`))

	got, err := h.store.GetArticle(ctx, articleID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummaryModel != "" {
		t.Errorf("deferred marker survived the switch to all: %q", got.SummaryModel)
	}
	var found bool
	for _, id := range h.enqueued()[before:] {
		if id == articleID {
			found = true
		}
	}
	if !found {
		t.Errorf("article %d was not enqueued after switching to all (enqueued %v)", articleID, h.enqueued())
	}
}

// The backfill is owed to a real change, in one direction only. Switching TO
// on-demand must not enqueue — going frugal is not a reason to spend
// inference — and re-saving 'all' when it is already 'all' must not re-queue
// the world, which is why the handler reads the prior mode before writing.
func TestAdminSettings_SummarizeMode_BackfillOnlyOnRealChangeToAll(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "admin", "p", true)
	cl := h.login(t, "admin", "p")

	ctx := context.Background()
	_, articleID := seedDeferredArticle(t, h, cl, "no-backfill")

	// Already 'all' (the default): saving 'all' again changes nothing.
	before := len(h.enqueued())
	patchAdminSettings(t, cl, h.srv.URL, []byte(`{"summarize_mode":"all"}`))
	if n := len(h.enqueued()) - before; n != 0 {
		t.Errorf("a no-op save of 'all' enqueued %d articles, want 0", n)
	}
	if got, _ := h.store.GetArticle(ctx, articleID); got.SummaryModel != "deferred" {
		t.Errorf("a no-op save cleared the deferred marker: %q", got.SummaryModel)
	}

	// Switching TO on-demand is not a backfill trigger either.
	before = len(h.enqueued())
	patchAdminSettings(t, cl, h.srv.URL, []byte(`{"summarize_mode":"on_demand"}`))
	if n := len(h.enqueued()) - before; n != 0 {
		t.Errorf("switching to on-demand enqueued %d articles, want 0", n)
	}
	if got, _ := h.store.GetArticle(ctx, articleID); got.SummaryModel != "deferred" {
		t.Errorf("switching to on-demand disturbed the article: %q", got.SummaryModel)
	}

	// A rejected write must not backfill on its way to the 400, either.
	before = len(h.enqueued())
	if code, _ := patchJSON(t, cl, h.srv.URL+"/api/admin/settings",
		[]byte(`{"summarize_mode":"bogus"}`)); code != http.StatusBadRequest {
		t.Fatalf("invalid mode = %d, want 400", code)
	}
	if n := len(h.enqueued()) - before; n != 0 {
		t.Errorf("a rejected write enqueued %d articles, want 0", n)
	}
}

// The per-feed override round-trips through GET /api/feeds, including back to
// "" — clearing an override is a real choice, not an absence of one.
func TestFeeds_PatchSummarizeMode_RoundTrips(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "p", false)
	cA := h.login(t, "alice", "p")
	subID, _ := addFeedFor(t, h, cA, "https://mode.test/feed")

	if got := feedFor(t, cA, h.srv.URL).SummarizeMode; got != store.ModeInherit {
		t.Fatalf("precondition: new subscriptions inherit, got %q", got)
	}

	for _, mode := range []string{store.ModeOnDemand, store.ModeAll, store.ModeInherit} {
		if code := patch(t, cA, fmt.Sprintf("%s/api/feeds/%d", h.srv.URL, subID),
			map[string]any{"summarize_mode": mode}, nil); code != http.StatusOK {
			t.Fatalf("patch %q = %d", mode, code)
		}
		if got := feedFor(t, cA, h.srv.URL).SummarizeMode; got != mode {
			t.Errorf("summarize_mode = %q, want %q", got, mode)
		}
	}
}

// Same contract as the server-wide setting: an unrecognized value is a 400
// naming the field, and the stored override is untouched.
func TestFeeds_PatchSummarizeMode_RejectsInvalid(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "p", false)
	cA := h.login(t, "alice", "p")
	subID, _ := addFeedFor(t, h, cA, "https://badmode.test/feed")

	if code := patch(t, cA, fmt.Sprintf("%s/api/feeds/%d", h.srv.URL, subID),
		map[string]any{"summarize_mode": store.ModeOnDemand}, nil); code != http.StatusOK {
		t.Fatalf("setup patch = %d", code)
	}
	for _, bad := range []string{"sometimes", "ON_DEMAND", "never"} {
		if code := patch(t, cA, fmt.Sprintf("%s/api/feeds/%d", h.srv.URL, subID),
			map[string]any{"summarize_mode": bad}, nil); code != http.StatusBadRequest {
			t.Errorf("patch %q = %d, want 400", bad, code)
		}
	}
	if got := feedFor(t, cA, h.srv.URL).SummarizeMode; got != store.ModeOnDemand {
		t.Errorf("a rejected write changed the stored override to %q", got)
	}
}

// Switching a feed to "every article" has to recover what on-demand mode left
// as 'deferred'. Without this the choice is inert until the feed publishes
// again — the same complaint the per-feed opt-out already had to fix once, and
// worse here, because an on-demand feed can sit for weeks accumulating
// deferred articles first.
func TestFeeds_PatchSummarizeMode_BackfillsDeferred(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "p", false)
	cA := h.login(t, "alice", "p")

	ctx := context.Background()
	if err := h.store.PutSummarizeMode(ctx, store.ModeOnDemand); err != nil {
		t.Fatal(err)
	}
	subID, articleID := seedDeferredArticle(t, h, cA, "backfill-me")

	before := len(h.enqueued())
	if code := patch(t, cA, fmt.Sprintf("%s/api/feeds/%d", h.srv.URL, subID),
		map[string]any{"summarize_mode": store.ModeAll}, nil); code != http.StatusOK {
		t.Fatalf("switch to all = %d", code)
	}

	got, err := h.store.GetArticle(ctx, articleID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummaryModel != "" {
		t.Errorf("deferred marker survived the switch to all: %q", got.SummaryModel)
	}
	var found bool
	for _, id := range h.enqueued()[before:] {
		if id == articleID {
			found = true
		}
	}
	if !found {
		t.Errorf("article %d was not enqueued after switching to all (enqueued %v)", articleID, h.enqueued())
	}
}

// The mirror image: switching a feed TO on-demand must not enqueue anything.
// The backfill is a one-way door — going frugal is not a reason to spend
// inference.
func TestFeeds_PatchSummarizeMode_OnDemandEnqueuesNothing(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "p", false)
	cA := h.login(t, "alice", "p")
	subID, articleID := seedDeferredArticle(t, h, cA, "stay-deferred")

	before := len(h.enqueued())
	if code := patch(t, cA, fmt.Sprintf("%s/api/feeds/%d", h.srv.URL, subID),
		map[string]any{"summarize_mode": store.ModeOnDemand}, nil); code != http.StatusOK {
		t.Fatalf("switch to on_demand = %d", code)
	}
	if n := len(h.enqueued()) - before; n != 0 {
		t.Errorf("enqueued %d articles switching to on-demand, want 0", n)
	}
	if got, _ := h.store.GetArticle(context.Background(), articleID); got.SummaryModel != "deferred" {
		t.Errorf("summary_model = %q, want still deferred", got.SummaryModel)
	}
}

// The per-feed mode is per-subscription, reachable only through the caller's
// own subscription id — the same guard the opt-out has, and for the same
// reason: it is a row in someone else's table.
func TestFeeds_PatchSummarizeMode_IsPerUser(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "p", false)
	h.seedUser(t, "bob", "p", false)
	cA := h.login(t, "alice", "p")
	cB := h.login(t, "bob", "p")

	const url = "https://sharedmode.test/feed"
	aliceSub, _ := addFeedFor(t, h, cA, url)
	addFeedFor(t, h, cB, url) // same shared feed row

	if code := patch(t, cB, fmt.Sprintf("%s/api/feeds/%d", h.srv.URL, aliceSub),
		map[string]any{"summarize_mode": store.ModeOnDemand}, nil); code != http.StatusNotFound {
		t.Errorf("bob patching alice's subscription = %d, want 404", code)
	}
	if got := feedFor(t, cA, h.srv.URL).SummarizeMode; got != store.ModeInherit {
		t.Errorf("the rejected patch reached alice's row: summarize_mode = %q", got)
	}
}

// PATCH /api/boards/{id} round-trips through GET /api/boards, which is what
// the sidebar renders the toggle from.
func TestBoards_PatchSummarize_RoundTrips(t *testing.T) {
	h := newHarness(t)
	u := h.seedUser(t, "alice", "p", false)
	cA := h.login(t, "alice", "p")

	board, err := h.store.CreateBoard(context.Background(), models.Board{UserID: u.ID, Name: "Filing"})
	if err != nil {
		t.Fatal(err)
	}

	var env struct {
		Data models.Board `json:"data"`
	}
	if code := patch(t, cA, fmt.Sprintf("%s/api/boards/%d", h.srv.URL, board.ID),
		map[string]any{"summarize": false}, &env); code != http.StatusOK {
		t.Fatalf("patch = %d", code)
	}
	if env.Data.Summarize {
		t.Error("patch echo still reports summarize = true")
	}

	var list struct {
		Data []models.Board `json:"data"`
	}
	get(t, cA, h.srv.URL+"/api/boards", &list)
	if len(list.Data) != 1 || list.Data[0].Summarize {
		t.Fatalf("after opt-out: %+v", list.Data)
	}
	if code := patch(t, cA, fmt.Sprintf("%s/api/boards/%d", h.srv.URL, board.ID),
		map[string]any{"summarize": true}, nil); code != http.StatusOK {
		t.Fatalf("re-enable = %d", code)
	}
	get(t, cA, h.srv.URL+"/api/boards", &list)
	if len(list.Data) != 1 || !list.Data[0].Summarize {
		t.Errorf("after opt-in: %+v", list.Data)
	}
}

// A board is a per-user resource. A foreign or unknown id must 404 — never
// leak that the board exists, and never mutate the owner's row. Checked
// against the owner's read-back rather than only the status code, because a
// handler that writes first and checks ownership afterwards would still return
// the right number.
func TestBoards_PatchSummarize_ForeignIDIs404(t *testing.T) {
	h := newHarness(t)
	alice := h.seedUser(t, "alice", "p", false)
	h.seedUser(t, "bob", "p", false)
	cA := h.login(t, "alice", "p")
	cB := h.login(t, "bob", "p")

	board, err := h.store.CreateBoard(context.Background(), models.Board{UserID: alice.ID, Name: "Alice's"})
	if err != nil {
		t.Fatal(err)
	}

	if code := patch(t, cB, fmt.Sprintf("%s/api/boards/%d", h.srv.URL, board.ID),
		map[string]any{"summarize": false}, nil); code != http.StatusNotFound {
		t.Errorf("bob patching alice's board = %d, want 404", code)
	}
	if code := patch(t, cB, fmt.Sprintf("%s/api/boards/%d", h.srv.URL, board.ID+9999),
		map[string]any{"summarize": false}, nil); code != http.StatusNotFound {
		t.Errorf("patching a nonexistent board = %d, want 404", code)
	}

	// Alice's board is exactly as she left it.
	var list struct {
		Data []models.Board `json:"data"`
	}
	get(t, cA, h.srv.URL+"/api/boards", &list)
	if len(list.Data) != 1 || !list.Data[0].Summarize {
		t.Errorf("the rejected patch reached alice's board: %+v", list.Data)
	}
	// And the store agrees — not just the list endpoint.
	if wants, err := h.store.BoardSummarizes(context.Background(), alice.ID, board.ID); err != nil || !wants {
		t.Errorf("BoardSummarizes = %v (err %v), want true", wants, err)
	}
}
