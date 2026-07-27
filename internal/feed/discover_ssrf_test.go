package feed

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Nothing may be fetched without passing validate first. This is the invariant
// the package exists to hold: callers pass an SSRF guard, and every URL the
// discovery code requests — target, rewritten target, alternate link, probe —
// must go through it. A prior version returned the <link rel="alternate"> URL
// unvalidated and relied on the caller to re-check.
func TestDiscovery_EveryFetchedURLIsValidated(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head>
<link rel="alternate" type="application/rss+xml" href="/alt.rss"></head><body>x</body></html>`))
	})
	mux.HandleFunc("/alt.rss", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte("<rss/>"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, fn := range []struct {
		name string
		call func(validate func(string) error) error
	}{
		{"Discover", func(v func(string) error) error {
			_, err := Discover(context.Background(), srv.Client(), srv.URL+"/", v)
			return err
		}},
		{"DiscoverAll", func(v func(string) error) error {
			_, err := DiscoverAll(context.Background(), srv.Client(), srv.URL+"/", v)
			return err
		}},
	} {
		t.Run(fn.name, func(t *testing.T) {
			// Record every URL offered to validate, and reject everything.
			// If any URL is fetched anyway the handlers would run — assert on
			// the server never being hit by checking the returned error.
			var offered []string
			validate := func(u string) error {
				offered = append(offered, u)
				return errors.New("blocked")
			}
			err := fn.call(validate)
			if err == nil {
				t.Fatal("all URLs blocked but discovery reported success")
			}
			if len(offered) == 0 {
				t.Fatal("nothing was offered to validate — a fetch bypassed the guard")
			}
			for _, u := range offered {
				if !strings.HasPrefix(u, srv.URL) {
					t.Errorf("unexpected URL offered to validate: %q", u)
				}
			}
		})
	}

	// With a validator that allows the homepage but blocks the alternate, the
	// alternate must NOT be returned — it would be fetched by the caller.
	t.Run("blocked alternate is not returned", func(t *testing.T) {
		validate := func(u string) error {
			if strings.HasSuffix(u, "/alt.rss") {
				return errors.New("blocked alternate")
			}
			return nil
		}
		got, err := Discover(context.Background(), srv.Client(), srv.URL+"/", validate)
		if err == nil && strings.HasSuffix(got, "/alt.rss") {
			t.Fatalf("Discover returned a validate-rejected URL: %q", got)
		}
		all, err := DiscoverAll(context.Background(), srv.Client(), srv.URL+"/", validate)
		if err != nil {
			t.Fatalf("DiscoverAll: %v", err)
		}
		for _, d := range all {
			if strings.HasSuffix(d.URL, "/alt.rss") {
				t.Errorf("DiscoverAll returned a validate-rejected URL: %q", d.URL)
			}
		}
	})
}
