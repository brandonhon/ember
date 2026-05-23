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

	got, err := Discover(context.Background(), srv.Client(), srv.URL)
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

	got, err := Discover(context.Background(), srv.Client(), srv.URL)
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

	got, err := Discover(context.Background(), srv.Client(), srv.URL+"/")
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

	_, err := Discover(context.Background(), srv.Client(), srv.URL+"/")
	if !errors.Is(err, ErrNoFeed) {
		t.Errorf("expected ErrNoFeed, got %v", err)
	}
}
