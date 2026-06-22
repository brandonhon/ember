package feed

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestParse_MediaRSSImage(t *testing.T) {
	// News publishers (Fox, Reuters, …) ship the lead image only via Media RSS,
	// which gofeed exposes through it.Extensions rather than it.Image. Cover
	// media:content (image), media:thumbnail fallback, and skipping video.
	body := []byte(`<?xml version="1.0"?>
<rss version="2.0" xmlns:media="http://search.yahoo.com/mrss/">
  <channel>
    <title>News</title>
    <item>
      <title>Has media:content image</title>
      <link>https://news.example/a</link>
      <description>Plain summary, no inline image.</description>
      <media:content url="https://img.example/lead.jpg" type="image/jpeg" width="931" height="523" />
    </item>
    <item>
      <title>Only a media:thumbnail</title>
      <link>https://news.example/b</link>
      <description>No content image.</description>
      <media:thumbnail url="https://img.example/thumb.jpg" />
    </item>
    <item>
      <title>media:content is a video</title>
      <link>https://news.example/c</link>
      <description>No usable image.</description>
      <media:content url="https://vid.example/clip.mp4" type="video/mp4" />
    </item>
  </channel>
</rss>`)
	res, err := Parse(context.Background(), 1, body, "https://news.example/feed.xml")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Articles) != 3 {
		t.Fatalf("articles = %d, want 3", len(res.Articles))
	}
	if got := res.Articles[0].ImageURL; !strings.Contains(got, "lead.jpg") {
		t.Errorf("media:content image not extracted: %q", got)
	}
	if got := res.Articles[1].ImageURL; !strings.Contains(got, "thumb.jpg") {
		t.Errorf("media:thumbnail not used as fallback: %q", got)
	}
	if got := res.Articles[2].ImageURL; got != "" {
		t.Errorf("video media:content should not be used as an image: %q", got)
	}
}

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

func TestParse_DecodesTitleEntities(t *testing.T) {
	// Atom title type="html": entities (curly quotes, ampersand) must be decoded
	// to display text, since titles render as plain text in the UI, not {@html}.
	body := []byte(`<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title type="html"><![CDATA[Tom &amp; Jerry &#8212; News]]></title>
  <entry>
    <title type="html"><![CDATA[Roblox exec says it is &#8216;not enough anymore&#8217;]]></title>
    <link rel="alternate" type="text/html" href="https://example.com/a"/>
    <id>tag:example.com,2026:a</id>
    <summary type="html"><![CDATA[body]]></summary>
  </entry>
</feed>`)
	res, err := Parse(context.Background(), 1, body, "https://example.com/feed.xml")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Tom & Jerry — News"; res.Title != want {
		t.Errorf("feed title = %q, want %q", res.Title, want)
	}
	if len(res.Articles) != 1 {
		t.Fatalf("articles = %d, want 1", len(res.Articles))
	}
	if want := "Roblox exec says it is ‘not enough anymore’"; res.Articles[0].Title != want {
		t.Errorf("article title = %q, want %q", res.Articles[0].Title, want)
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
