package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/models"
)

// allowPrivate=true so httptest's 127.0.0.1 origin isn't SSRF-blocked.
func newTestProxy() *imageProxy {
	return newImageProxy("0123456789abcdef0123456789abcdef", true)
}

func TestImageProxyRewriteEmpty(t *testing.T) {
	if got := newTestProxy().rewrite(""); got != "" {
		t.Fatalf("rewrite(empty) = %q, want empty", got)
	}
}

func TestImageProxyRewritePassesThroughNonHTTP(t *testing.T) {
	p := newTestProxy()
	src := "data:image/png;base64,AAAA"
	if got := p.rewrite(src); got != src {
		t.Fatalf("rewrite(data URI) = %q, want passthrough", got)
	}
}

func TestImageProxyRewriteSignsHTTP(t *testing.T) {
	got := newTestProxy().rewrite("https://cdn.example.com/x.png?a=1")
	if !strings.HasPrefix(got, "/api/img?") {
		t.Fatalf("rewrite = %q, want /api/img? prefix", got)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("u") != "https://cdn.example.com/x.png?a=1" {
		t.Fatalf("u param = %q", u.Query().Get("u"))
	}
	if u.Query().Get("s") == "" {
		t.Fatal("missing signature")
	}
}

func TestImageProxyServesSignedImage(t *testing.T) {
	const body = "\x89PNG\r\n\x1a\nfake-bytes"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.WriteString(w, body)
	}))
	defer origin.Close()

	p := newTestProxy()
	req := httptest.NewRequest(http.MethodGet, p.rewrite(origin.URL+"/lead.png"), nil)
	rr := httptest.NewRecorder()
	p.handle(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%q)", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content-type = %q, want image/png", ct)
	}
	if rr.Body.String() != body {
		t.Fatalf("body mismatch: got %q", rr.Body.String())
	}
	if rr.Header().Get("Cache-Control") == "" {
		t.Fatal("missing Cache-Control")
	}
}

func TestImageProxyRejectsBadSignature(t *testing.T) {
	var hit bool
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
	}))
	defer origin.Close()

	q := url.Values{}
	q.Set("u", origin.URL+"/lead.png")
	q.Set("s", "tampered-signature")
	req := httptest.NewRequest(http.MethodGet, "/api/img?"+q.Encode(), nil)
	rr := httptest.NewRecorder()
	newTestProxy().handle(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
	if hit {
		t.Fatal("origin was fetched despite bad signature")
	}
}

// TestArticleResponseRewritesImageURL is the end-to-end wiring check: an
// article with a publisher-CDN image_url comes back through the real router
// with image_url rewritten to a signed same-origin /api/img path.
func TestArticleResponseRewritesImageURL(t *testing.T) {
	h := newHarness(t)
	u := h.seedUser(t, "alice", "p", false)
	c := h.login(t, "alice", "p")

	ctx := context.Background()
	f, _ := h.store.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})
	_, _ = h.store.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f.ID})
	const raw = "https://cdn.test/lead.png?ve=1"
	a, _, _ := h.store.UpsertArticle(ctx, models.Article{
		FeedID: f.ID, GUID: "img1", Title: "Has Image", ContentHash: "h1",
		PublishedAt: time.Now().Add(-time.Hour).Unix(), ImageURL: raw,
	})

	var resp struct {
		Data models.ArticleView `json:"data"`
	}
	if code := get(t, c, fmt.Sprintf("%s/api/articles/%d", h.srv.URL, a.ID), &resp); code != http.StatusOK {
		t.Fatalf("get article = %d", code)
	}
	if !strings.HasPrefix(resp.Data.ImageURL, "/api/img?") {
		t.Fatalf("image_url not proxied: %q", resp.Data.ImageURL)
	}
	parsed, err := url.Parse(resp.Data.ImageURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("u") != raw {
		t.Fatalf("proxied u = %q, want %q", parsed.Query().Get("u"), raw)
	}
}

func TestImageProxyRejectsNonImage(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html>")
	}))
	defer origin.Close()

	p := newTestProxy()
	req := httptest.NewRequest(http.MethodGet, p.rewrite(origin.URL+"/notimage"), nil)
	rr := httptest.NewRecorder()
	p.handle(rr, req)

	if rr.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415 (body=%q)", rr.Code, rr.Body.String())
	}
}
