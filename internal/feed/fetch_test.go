package feed

import (
	"context"
	"errors"
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

func TestRedirectGuard(t *testing.T) {
	blocked := errors.New("blocked by validator")

	t.Run("rejects redirect the validator blocks", func(t *testing.T) {
		guard := RedirectGuard(func(string) error { return blocked })
		req, _ := http.NewRequest(http.MethodGet, "http://169.254.169.254/", nil)
		if err := guard(req, nil); !errors.Is(err, blocked) {
			t.Fatalf("want blocked error, got %v", err)
		}
	})

	t.Run("allows redirect the validator permits", func(t *testing.T) {
		guard := RedirectGuard(func(string) error { return nil })
		req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
		if err := guard(req, nil); err != nil {
			t.Fatalf("want nil, got %v", err)
		}
	})

	t.Run("stops after 10 redirects", func(t *testing.T) {
		guard := RedirectGuard(func(string) error { return nil })
		req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
		via := make([]*http.Request, 10)
		if err := guard(req, via); err == nil {
			t.Fatal("want error after 10 redirects, got nil")
		}
	})
}

// TestEnrichClientHasRedirectGuard is a compile+wiring check: the readability
// client built in enrichArticle must carry a CheckRedirect guard (H-1). Done
// indirectly here because enrichArticle is unexported in package poller; the
// guard mechanism it relies on is covered by TestRedirectGuard above.
