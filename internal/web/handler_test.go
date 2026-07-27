package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func handler(t *testing.T) http.Handler {
	t.Helper()
	h, err := Handler()
	if err != nil {
		t.Skipf("dist not embedded (run `make embed`): %v", err)
	}
	return h
}

// Directory requests must not be listed. Go's FileServer renders a listing for
// any directory without an index.html — /assets/ would enumerate every built
// asset filename, and because the /assets/ prefix sets an immutable
// year-long cache header, that listing would be cached as if it were a
// content-hashed file.
func TestHandler_NoDirectoryListing(t *testing.T) {
	h := handler(t)
	for _, p := range []string{"/assets/", "/assets"} {
		rec := get(t, h, p)
		if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "<pre>") {
			t.Errorf("%s returned a directory listing:\n%s", p, rec.Body.String())
		}
		if rec.Code == http.StatusOK {
			t.Errorf("%s = 200, want a non-200 (no listing)", p)
		}
	}
}

// Content-hashed assets get the long immutable cache; the shell never does.
func TestHandler_CacheControlMatrix(t *testing.T) {
	h := handler(t)
	for _, tc := range []struct{ path, wantCC, why string }{
		{"/", "no-cache, must-revalidate", "shell must revalidate — Firefox heuristic-caches HTML for up to 24h otherwise"},
		{"/reader", "no-cache, must-revalidate", "SPA history fallback is the shell"},
		{"/deeply/nested/route", "no-cache, must-revalidate", "any extensionless unknown path is the shell"},
		{"/sw.js", "no-cache", "service worker updates must roll out on next load"},
	} {
		rec := get(t, h, tc.path)
		if got := rec.Header().Get("Cache-Control"); got != tc.wantCC {
			t.Errorf("%s Cache-Control = %q, want %q (%s)", tc.path, got, tc.wantCC, tc.why)
		}
	}
}

// A real hashed asset is served immutable — that is the whole point of Vite's
// content-hashed filenames.
func TestHandler_HashedAssetIsImmutable(t *testing.T) {
	h := handler(t)
	body := get(t, h, "/").Body.String()
	i := strings.Index(body, "/assets/")
	if i < 0 {
		t.Skip("index.html references no /assets/ file")
	}
	rest := body[i:]
	end := strings.IndexAny(rest, `"'`)
	if end < 0 {
		t.Skip("could not extract an asset path")
	}
	asset := rest[:end]

	rec := get(t, h, asset)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", asset, rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("%s Cache-Control = %q, want immutable", asset, got)
	}
}

// .webmanifest needs an explicit type — Go's mime.TypeByExtension doesn't know it.
func TestHandler_WebmanifestContentType(t *testing.T) {
	h := handler(t)
	rec := get(t, h, "/manifest.webmanifest")
	if rec.Code != http.StatusOK {
		t.Skipf("no manifest in this build (%d)", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/manifest+json" {
		t.Errorf("Content-Type = %q, want application/manifest+json", got)
	}
}

// Traversal must never escape the embedded FS, and a missing file with an
// extension is a 404 rather than a silent shell response (which would make
// broken asset URLs look like they worked).
func TestHandler_TraversalAndMissingFiles(t *testing.T) {
	h := handler(t)
	for _, p := range []string{"/../etc/passwd", "/../../etc/passwd", "/assets/../../etc/passwd"} {
		rec := get(t, h, p)
		if strings.Contains(rec.Body.String(), "root:") {
			t.Errorf("%s escaped the embedded FS", p)
		}
	}
	rec := get(t, h, "/nonexistent.txt")
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing file with an extension = %d, want 404", rec.Code)
	}
}
