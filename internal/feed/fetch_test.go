package feed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetch_ConditionalGET_304(t *testing.T) {
	const etag = `"v1"`
	const lastMod = "Wed, 21 Oct 2026 07:28:00 GMT"

	var saw struct {
		ifNoneMatch string
		ifModSince  string
		userAgent   string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		saw.ifNoneMatch = r.Header.Get("If-None-Match")
		saw.ifModSince = r.Header.Get("If-Modified-Since")
		saw.userAgent = r.Header.Get("User-Agent")
		if r.Header.Get("If-None-Match") == etag {
			w.Header().Set("ETag", etag)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Header().Set("ETag", etag)
		w.Header().Set("Last-Modified", lastMod)
		_, _ = w.Write([]byte("<rss></rss>"))
	}))
	defer srv.Close()

	f := NewFetcher(5 * time.Second)
	res, err := f.Fetch(context.Background(), srv.URL, etag, lastMod)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Errorf("expected 304 (Changed=false), got %+v", res)
	}
	if res.StatusCode != http.StatusNotModified {
		t.Errorf("status = %d", res.StatusCode)
	}
	if saw.ifNoneMatch != etag {
		t.Errorf("If-None-Match = %q", saw.ifNoneMatch)
	}
	if saw.ifModSince != lastMod {
		t.Errorf("If-Modified-Since = %q", saw.ifModSince)
	}
	if !strings.HasPrefix(saw.userAgent, "ember/") {
		t.Errorf("User-Agent = %q", saw.userAgent)
	}
}

func TestFetch_200ReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		w.Header().Set("ETag", `"new"`)
		_, _ = w.Write([]byte("<feed></feed>"))
	}))
	defer srv.Close()

	f := NewFetcher(5 * time.Second)
	res, err := f.Fetch(context.Background(), srv.URL, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Error("expected Changed=true")
	}
	if string(res.Body) != "<feed></feed>" {
		t.Errorf("body = %q", string(res.Body))
	}
	if res.ETag != `"new"` {
		t.Errorf("ETag = %q", res.ETag)
	}
}

func TestFetch_5xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := NewFetcher(5 * time.Second)
	_, err := f.Fetch(context.Background(), srv.URL, "", "")
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if !IsTransientError(err) {
		t.Errorf("5xx should be transient")
	}
}

func TestFetch_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()

	f := NewFetcher(50 * time.Millisecond)
	_, err := f.Fetch(context.Background(), srv.URL, "", "")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
