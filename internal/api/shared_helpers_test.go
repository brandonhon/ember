package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/summarize"
)

// Add-feed and edit-feed both resolve their URL through resolveFeedURL, so the
// SSRF guard must reject a private target on either path — not just the one it
// was originally written for.
func TestFeeds_AddAndUpdateRejectPrivateURL(t *testing.T) {
	h := newHarnessWith(t, func(d *Dependencies) { d.AllowPrivateURLs = false })
	u := h.seedUser(t, "alice", "p", false)
	c := h.login(t, "alice", "p")

	// Literal IPs so the check never depends on DNS.
	if code := post(t, c, h.srv.URL+"/api/feeds",
		map[string]any{"url": "http://127.0.0.1/feed"}, nil); code != http.StatusBadRequest {
		t.Errorf("add private feed = %d, want 400", code)
	}

	f, _ := h.store.UpsertFeed(context.Background(), models.Feed{URL: "https://x.test/feed", Title: "X"})
	sub, _ := h.store.Subscribe(context.Background(), models.Subscription{UserID: u.ID, FeedID: f.ID})
	if code := patch(t, c, fmt.Sprintf("%s/api/feeds/%d", h.srv.URL, sub.ID),
		map[string]any{"url": "http://169.254.169.254/latest/meta-data"}, nil); code != http.StatusBadRequest {
		t.Errorf("repoint to private URL = %d, want 400", code)
	}
}

// viewFreshAfter backs both the article list and mark-all-read, so a named view
// must bound what mark-all-read touches the same way it bounds the list: an
// article outside the Fresh window stays unread.
func TestMarkAllRead_HonorsViewWindow(t *testing.T) {
	h := newHarness(t)
	u := h.seedUser(t, "alice", "p", false)
	c := h.login(t, "alice", "p")

	f, _ := h.store.UpsertFeed(context.Background(), models.Feed{URL: "https://x.test/feed", Title: "X"})
	_, _ = h.store.Subscribe(context.Background(), models.Subscription{UserID: u.ID, FeedID: f.ID})
	now := time.Now()
	for _, a := range []struct {
		guid string
		age  time.Duration
	}{{"old", 48 * time.Hour}, {"new", time.Hour}} {
		if _, _, err := h.store.UpsertArticle(context.Background(), models.Article{
			FeedID: f.ID, GUID: a.guid, Title: a.guid, ContentHash: a.guid,
			PublishedAt: now.Add(-a.age).Unix(), SummaryModel: "noop",
		}); err != nil {
			t.Fatal(err)
		}
	}

	var marked struct {
		Data struct {
			Count int64 `json:"count"`
		} `json:"data"`
	}
	if code := post(t, c, h.srv.URL+"/api/articles/mark-all-read",
		map[string]any{"view": "fresh"}, &marked); code != http.StatusOK {
		t.Fatalf("mark-all-read view=fresh = %d, want 200", code)
	}
	if marked.Data.Count != 1 {
		t.Errorf("marked %d articles, want 1 (only the one inside the Fresh window)", marked.Data.Count)
	}

	// The out-of-window article is still unread and still listed.
	var list struct {
		Data []models.ArticleView `json:"data"`
	}
	get(t, c, h.srv.URL+"/api/articles?unread=1", &list)
	if len(list.Data) != 1 || list.Data[0].GUID != "old" {
		t.Errorf("unread after mark-all-read view=fresh: %+v, want just \"old\"", list.Data)
	}
}

// Every admin LLM endpoint runs through requireSummarizer; with no Ollama wired
// (the default in tests) each must 503 rather than nil-panic. GET /admin/llm is
// the exception — it reports enabled=false so the UI can render the setup hint.
func TestLLM_EndpointsWithoutSummarizer(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "root", "hunter2", true)
	c := h.login(t, "root", "hunter2")

	var status struct {
		Data llmStatus `json:"data"`
	}
	if code := get(t, c, h.srv.URL+"/api/admin/llm", &status); code != http.StatusOK {
		t.Fatalf("GET /api/admin/llm = %d, want 200", code)
	}
	if status.Data.Enabled {
		t.Error("enabled=true with no summarizer configured")
	}

	for _, path := range []string{"/api/admin/llm/model", "/api/admin/llm/pull", "/api/admin/llm/delete"} {
		if code := post(t, c, h.srv.URL+path, map[string]any{"model": "llama3"}, nil); code != http.StatusServiceUnavailable {
			t.Errorf("POST %s = %d, want 503", path, code)
		}
	}
	if code := post(t, c, h.srv.URL+"/api/admin/llm/options",
		map[string]any{"temperature": 0.5}, nil); code != http.StatusServiceUnavailable {
		t.Errorf("POST /api/admin/llm/options = %d, want 503", code)
	}
}

// modelFromRequest rejects a missing or malformed model reference before it
// reaches the Ollama daemon. Exercised with a summarizer wired so the 503 from
// requireSummarizer can't mask the validation.
func TestLLM_ModelNameValidation(t *testing.T) {
	h := newHarnessWith(t, func(d *Dependencies) { d.Ollama = summarize.NewOllama("http://ollama.invalid", "llama3") })
	h.seedUser(t, "root", "hunter2", true)
	c := h.login(t, "root", "hunter2")

	for _, tc := range []struct {
		name  string
		model string
	}{
		{"empty", ""},
		{"path traversal", "../../etc/passwd"},
		{"leading dash", "-rf"},
		{"space", "llama3 evil"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code := post(t, c, h.srv.URL+"/api/admin/llm/model",
				map[string]any{"model": tc.model}, nil); code != http.StatusBadRequest {
				t.Errorf("POST model=%q = %d, want 400", tc.model, code)
			}
		})
	}
}

// The passkey begin/finish endpoints share one request and one response type
// across the register and login ceremonies. Without EMBER_PUBLIC_URL there's no
// WebAuthn config, so all four must 503 instead of nil-panicking.
func TestPasskeys_WithoutWebAuthn(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "p", false)
	c := h.login(t, "alice", "p")

	for _, path := range []string{
		"/api/me/passkeys/register/begin",
		"/api/me/passkeys/register/finish",
		"/api/auth/passkey/begin",
		"/api/auth/passkey/finish",
	} {
		if code := post(t, c, h.srv.URL+path,
			map[string]any{"username": "alice", "session_id": "x"}, nil); code != http.StatusServiceUnavailable {
			t.Errorf("POST %s = %d, want 503", path, code)
		}
	}
}
