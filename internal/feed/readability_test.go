package feed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/go-shiori/go-readability"
)

func TestExtractFromHTML(t *testing.T) {
	body, err := os.ReadFile("testdata/article.html")
	if err != nil {
		t.Fatal(err)
	}
	r, err := extractFromHTML(string(body), "https://example.com/article")
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "How to brew espresso at home" {
		t.Errorf("title = %q", r.Title)
	}
	if !strings.Contains(r.Text, "espresso") {
		t.Errorf("text missing core content: %q", r.Text)
	}
	// Navigation boilerplate should NOT survive readability.
	if strings.Contains(r.Text, "Home | About | Contact") {
		t.Errorf("boilerplate not removed: %q", r.Text)
	}
}

func TestExtractFromURL(t *testing.T) {
	body, _ := os.ReadFile("testdata/article.html")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	r, err := ExtractFromURL(context.Background(), srv.Client(), srv.URL+"/article")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Text, "espresso") {
		t.Errorf("missing content: %q", r.Text)
	}
}

func TestExtractFromURL_Non2xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := ExtractFromURL(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Error("expected error for 404")
	}
}

// extractFromHTML runs readability over a literal HTML body with no HTTP
// request. Test-only: it lives here rather than in readability.go so the
// production build carries no unreachable helper.
func extractFromHTML(body, target string) (Readable, error) {
	u, _ := url.Parse(target)
	art, err := readability.FromReader(strings.NewReader(body), u)
	if err != nil {
		return Readable{}, err
	}
	return readableFrom(art), nil
}
