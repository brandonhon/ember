package updatecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsReleaseVersion(t *testing.T) {
	cases := map[string]bool{
		"v0.9.4":           true,
		"v1.0.0":           true,
		"v0.9.4-5-gabc123": false, // between-tag `git describe`
		"v0.9.4-dirty":     false, // dirty tree
		"0.9.4":            false, // missing v prefix
		"dev":              false, // fallback build var
		"abc1234":          false, // bare SHA
		"":                 false,
		"v0.9.4+build.1":   false, // build metadata → treat as non-clean
	}
	for v, want := range cases {
		if got := IsReleaseVersion(v); got != want {
			t.Errorf("IsReleaseVersion(%q) = %v, want %v", v, got, want)
		}
	}
}

// stubGitHub returns a test server serving the given latest-release JSON and a
// Checker pointed at it.
func stubGitHub(t *testing.T, current, body string, status int) *Checker {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request missing User-Agent header")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := New(current, "brandonhon/ember", nil)
	c.baseURL = srv.URL
	return c
}

func TestCheckOnce_UpdateAvailable(t *testing.T) {
	body := `{"tag_name":"v0.9.5","html_url":"https://example.test/releases/v0.9.5","published_at":"2026-07-01T00:00:00Z"}`
	c := stubGitHub(t, "v0.9.4", body, http.StatusOK)
	if err := c.checkOnce(context.Background()); err != nil {
		t.Fatalf("checkOnce: %v", err)
	}
	res, ok := c.Latest()
	if !ok {
		t.Fatal("expected a cached result")
	}
	if !res.Available {
		t.Errorf("expected Available=true for v0.9.5 > v0.9.4")
	}
	if res.Latest != "v0.9.5" || res.URL != "https://example.test/releases/v0.9.5" {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestCheckOnce_UpToDate(t *testing.T) {
	body := `{"tag_name":"v0.9.4","html_url":"https://example.test/releases/v0.9.4","published_at":"2026-07-01T00:00:00Z"}`
	c := stubGitHub(t, "v0.9.4", body, http.StatusOK)
	if err := c.checkOnce(context.Background()); err != nil {
		t.Fatalf("checkOnce: %v", err)
	}
	res, ok := c.Latest()
	if !ok {
		t.Fatal("expected a cached result")
	}
	if res.Available {
		t.Errorf("expected Available=false when running the latest release")
	}
}

func TestCheckOnce_DevBuildSkips(t *testing.T) {
	// A dev build must not even hit the network, and never reports a result.
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	c := New("dev", "brandonhon/ember", nil)
	c.baseURL = srv.URL
	if err := c.checkOnce(context.Background()); err != nil {
		t.Fatalf("checkOnce: %v", err)
	}
	if called {
		t.Error("dev build should not call GitHub")
	}
	if _, ok := c.Latest(); ok {
		t.Error("dev build should have no cached result")
	}
}

func TestCheckOnce_ErrorKeepsPreviousResult(t *testing.T) {
	// Seed a good result, then a failing check must not clobber it.
	body := `{"tag_name":"v0.9.5","html_url":"https://example.test/v0.9.5","published_at":"2026-07-01T00:00:00Z"}`
	c := stubGitHub(t, "v0.9.4", body, http.StatusOK)
	if err := c.checkOnce(context.Background()); err != nil {
		t.Fatalf("seed checkOnce: %v", err)
	}
	c.baseURL = "http://127.0.0.1:0" // unroutable
	if err := c.checkOnce(context.Background()); err == nil {
		t.Fatal("expected an error from the unreachable endpoint")
	}
	res, ok := c.Latest()
	if !ok || res.Latest != "v0.9.5" {
		t.Errorf("previous result should survive an error; got %+v ok=%v", res, ok)
	}
}

func TestCheckOnce_InvalidTag(t *testing.T) {
	c := stubGitHub(t, "v0.9.4", `{"tag_name":"not-a-version"}`, http.StatusOK)
	if err := c.checkOnce(context.Background()); err == nil {
		t.Error("expected an error for a non-semver tag")
	}
}
