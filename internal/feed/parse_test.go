package feed

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestParse_RSS(t *testing.T) {
	body, _ := os.ReadFile("testdata/sample.rss")
	res, err := Parse(context.Background(), 1, body, "https://example.com/blog.rss")
	if err != nil {
		t.Fatal(err)
	}
	if res.Title != "Example Tech Blog" {
		t.Errorf("title = %q", res.Title)
	}
	if len(res.Articles) != 2 {
		t.Fatalf("articles = %d, want 2", len(res.Articles))
	}
	a := res.Articles[0]
	if a.Title != "Hello world from RSS" {
		t.Errorf("article[0] title = %q", a.Title)
	}
	if a.URL != "https://example.com/posts/hello" {
		t.Errorf("article[0] url = %q", a.URL)
	}
	if a.PublishedAt == 0 {
		t.Error("published_at not set")
	}
	if !strings.Contains(a.ContentText, "first") {
		t.Errorf("content_text missing 'first': %q", a.ContentText)
	}
	if !strings.Contains(a.ContentHTML, "<b>first</b>") {
		t.Errorf("content_html lost HTML tags: %q", a.ContentHTML)
	}
	if a.ImageURL == "" || !strings.Contains(a.ImageURL, "cover.png") {
		t.Errorf("first image not extracted: %q", a.ImageURL)
	}
	if a.ContentHash == "" {
		t.Error("content_hash empty")
	}

	// Relative link in second item should be resolved against feed URL.
	if !strings.Contains(res.Articles[1].URL, "example.com") {
		t.Errorf("relative URL not resolved: %q", res.Articles[1].URL)
	}
}

func TestParse_Atom(t *testing.T) {
	body, _ := os.ReadFile("testdata/sample.atom")
	res, err := Parse(context.Background(), 2, body, "https://atom.example.com/feed.xml")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Articles) != 1 {
		t.Fatalf("articles = %d, want 1", len(res.Articles))
	}
	a := res.Articles[0]
	if a.Title != "Atom item one" {
		t.Errorf("title = %q", a.Title)
	}
	if a.Author != "Bob Builder" {
		t.Errorf("author = %q", a.Author)
	}
	if a.URL != "https://atom.example.com/items/1" {
		t.Errorf("url = %q", a.URL)
	}
	if a.PublishedAt == 0 {
		t.Error("published_at missing")
	}
}

func TestParse_HashStability(t *testing.T) {
	body, _ := os.ReadFile("testdata/sample.rss")
	r1, _ := Parse(context.Background(), 1, body, "https://example.com/blog.rss")
	r2, _ := Parse(context.Background(), 1, body, "https://example.com/blog.rss")
	if r1.Articles[0].ContentHash != r2.Articles[0].ContentHash {
		t.Errorf("content_hash unstable: %q vs %q",
			r1.Articles[0].ContentHash, r2.Articles[0].ContentHash)
	}
}

func TestParse_EmptyBodyError(t *testing.T) {
	if _, err := Parse(context.Background(), 1, nil, ""); err == nil {
		t.Error("expected error for empty body")
	}
}

func TestParse_HashDiffersAcrossArticles(t *testing.T) {
	body, _ := os.ReadFile("testdata/sample.rss")
	res, _ := Parse(context.Background(), 1, body, "https://example.com/blog.rss")
	if res.Articles[0].ContentHash == res.Articles[1].ContentHash {
		t.Errorf("hash should differ across articles")
	}
}

func TestContentHash_Deterministic(t *testing.T) {
	a := ContentHash("https://x", "t", "body")
	b := ContentHash("https://x", "t", "body")
	if a != b {
		t.Errorf("ContentHash not deterministic")
	}
	c := ContentHash("https://x", "t", "body 2")
	if a == c {
		t.Errorf("ContentHash collision")
	}
}
