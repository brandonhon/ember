package feed

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// allowAll is the no-op validator used in tests where the SSRF guard isn't
// the subject under test — Discover requires a non-nil validator.
var allowAll = func(string) error { return nil }

func TestDiscover_FromLinkAlternate(t *testing.T) {
	body, err := os.ReadFile("testdata/site_with_feed.html")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	got, err := Discover(context.Background(), srv.Client(), srv.URL, allowAll)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	// /blog.rss is relative, should be resolved to the absolute test-server URL.
	if !strings.HasSuffix(got, "/blog.rss") {
		t.Errorf("got %q, want suffix /blog.rss", got)
	}
	if !strings.HasPrefix(got, "http") {
		t.Errorf("href should be absolute: %q", got)
	}
}

func TestDiscover_DirectFeedContentType(t *testing.T) {
	rss, _ := os.ReadFile("testdata/sample.rss")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(rss)
	}))
	defer srv.Close()

	got, err := Discover(context.Background(), srv.Client(), srv.URL, allowAll)
	if err != nil {
		t.Fatal(err)
	}
	if got != srv.URL {
		t.Errorf("Discover should return target itself for direct feed URL: %q != %q", got, srv.URL)
	}
}

func TestDiscover_FallbackPathHit(t *testing.T) {
	rss, _ := os.ReadFile("testdata/sample.rss")
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>no link tags here</body></html>"))
	})
	mux.HandleFunc("/feed", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(rss)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	got, err := Discover(context.Background(), srv.Client(), srv.URL+"/", allowAll)
	if err != nil {
		t.Fatalf("Discover fallback: %v", err)
	}
	if !strings.HasSuffix(got, "/feed") {
		t.Errorf("got %q, want fallback /feed", got)
	}
}

func TestDiscover_NoFeedFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>plain page</body></html>"))
	})
	// Every fallback path 404s.
	mux.HandleFunc("/feed", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404) })
	mux.HandleFunc("/rss", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404) })
	mux.HandleFunc("/atom.xml", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404) })
	mux.HandleFunc("/feed.xml", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404) })
	mux.HandleFunc("/index.xml", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, err := Discover(context.Background(), srv.Client(), srv.URL+"/", allowAll)
	if !errors.Is(err, ErrNoFeed) {
		t.Errorf("expected ErrNoFeed, got %v", err)
	}
}

func TestDiscover_NilValidateRejected(t *testing.T) {
	_, err := Discover(context.Background(), http.DefaultClient, "https://example.com", nil)
	if err == nil || !strings.Contains(err.Error(), "validate") {
		t.Errorf("nil validate should error with mention of validate, got %v", err)
	}
}

func TestDiscover_ValidateBlocksTarget(t *testing.T) {
	// Validator returns an error -> no request should fire.
	blocked := errors.New("blocked by SSRF guard")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("server should not have been hit; validator should reject first")
		w.WriteHeader(500)
	}))
	defer srv.Close()
	_, err := Discover(context.Background(), srv.Client(), srv.URL, func(string) error { return blocked })
	if err == nil || !errors.Is(err, blocked) {
		t.Errorf("want wrapped blocked error, got %v", err)
	}
}

func TestDiscover_ValidateBlocksProbe(t *testing.T) {
	// First fetch (the homepage) is allowed and returns plain HTML with no
	// alternate link. The probes should be attempted but the validator
	// rejects them, so we expect ErrNoFeed without /feed et al being hit.
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>nothing</body></html>"))
	})
	for _, p := range DiscoveryFallbacks {
		mux.HandleFunc(p, func(w http.ResponseWriter, _ *http.Request) {
			t.Errorf("probe %s should have been blocked by validator", p)
			w.WriteHeader(500)
		})
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Allow the homepage, reject anything else.
	first := true
	validate := func(string) error {
		if first {
			first = false
			return nil
		}
		return errors.New("probe blocked")
	}
	_, err := Discover(context.Background(), srv.Client(), srv.URL+"/", validate)
	if !errors.Is(err, ErrNoFeed) {
		t.Errorf("want ErrNoFeed (all probes blocked), got %v", err)
	}
}

const multiFeedHTML = `<html><head>
<link rel="alternate" type="application/rss+xml" title="Main RSS" href="/main.rss">
<link rel="alternate" type="application/atom+xml" title="Comments" href="/comments.atom">
<link rel="stylesheet" href="/style.css">
</head><body>page</body></html>`

func TestDiscoverAll_MultipleFeeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(multiFeedHTML))
	}))
	defer srv.Close()

	got, err := DiscoverAll(context.Background(), srv.Client(), srv.URL, allowAll)
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 feeds, got %d: %+v", len(got), got)
	}
	if !strings.HasSuffix(got[0].URL, "/main.rss") || got[0].Title != "Main RSS" || got[0].Type != "rss" {
		t.Errorf("feed[0] wrong: %+v", got[0])
	}
	if !strings.HasSuffix(got[1].URL, "/comments.atom") || got[1].Type != "atom" {
		t.Errorf("feed[1] wrong: %+v", got[1])
	}
	for _, f := range got {
		if !strings.HasPrefix(f.URL, "http") {
			t.Errorf("URL should be absolute: %q", f.URL)
		}
	}
}

func TestDiscoverAll_DropsSSRFRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(multiFeedHTML))
	}))
	defer srv.Close()

	// Reject the comments feed (simulating an SSRF-blocked target); allow the rest.
	validate := func(raw string) error {
		if strings.Contains(raw, "comments.atom") {
			return errors.New("blocked")
		}
		return nil
	}
	got, err := DiscoverAll(context.Background(), srv.Client(), srv.URL, validate)
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}
	if len(got) != 1 || !strings.HasSuffix(got[0].URL, "/main.rss") {
		t.Fatalf("want only /main.rss after dropping rejected feed, got %+v", got)
	}
}

func TestDiscoverAll_DirectFeed(t *testing.T) {
	rss, _ := os.ReadFile("testdata/sample.rss")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(rss)
	}))
	defer srv.Close()

	got, err := DiscoverAll(context.Background(), srv.Client(), srv.URL, allowAll)
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}
	if len(got) != 1 || got[0].URL != srv.URL || got[0].Type != "rss" {
		t.Fatalf("direct feed should return single rss entry for target, got %+v", got)
	}
}

func TestDiscoverAll_NoFeedsEmpty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>plain page</body></html>"))
	})
	for _, p := range DiscoveryFallbacks {
		mux.HandleFunc(p, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404) })
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	got, err := DiscoverAll(context.Background(), srv.Client(), srv.URL+"/", allowAll)
	if err != nil {
		t.Fatalf("DiscoverAll should not error on no-feed, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty slice, got %+v", got)
	}
}
