package feed

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html"

	"github.com/brandonhon/ember/internal/models"
)

// ParseResult is the normalized output of parsing a feed body.
type ParseResult struct {
	Title    string
	SiteURL  string
	Articles []models.Article
}

// Parse parses a feed body (RSS or Atom) and returns normalized articles. The
// feedID is stamped on every produced article; published_at falls back through
// updated_at; content_text is derived by stripping HTML; content_hash is a
// stable digest of (url|title|content_text).
func Parse(_ context.Context, feedID int64, body []byte, sourceURL string) (ParseResult, error) {
	if len(body) == 0 {
		return ParseResult{}, errors.New("feed: empty body")
	}
	fp := gofeed.NewParser()
	parsed, err := fp.ParseString(string(body))
	if err != nil {
		return ParseResult{}, err
	}

	base, _ := url.Parse(sourceURL)
	out := ParseResult{
		Title:   strings.TrimSpace(parsed.Title),
		SiteURL: strings.TrimSpace(parsed.Link),
	}
	if base != nil && out.SiteURL != "" {
		if u, err := base.Parse(out.SiteURL); err == nil {
			out.SiteURL = u.String()
		}
	}

	for _, it := range parsed.Items {
		a := normalizeItem(it, feedID, base)
		out.Articles = append(out.Articles, a)
	}
	return out, nil
}

func normalizeItem(it *gofeed.Item, feedID int64, base *url.URL) models.Article {
	a := models.Article{FeedID: feedID}
	a.GUID = strings.TrimSpace(it.GUID)
	if a.GUID == "" {
		a.GUID = strings.TrimSpace(it.Link)
	}
	a.Title = strings.TrimSpace(it.Title)
	a.URL = resolveLink(base, it.Link)

	switch {
	case it.Content != "":
		a.ContentHTML = it.Content
	case it.Description != "":
		a.ContentHTML = it.Description
	}
	a.ContentText = htmlToText(a.ContentHTML)

	if it.Author != nil {
		switch {
		case it.Author.Name != "":
			a.Author = it.Author.Name
		case it.Author.Email != "":
			a.Author = it.Author.Email
		}
	} else if len(it.Authors) > 0 && it.Authors[0] != nil {
		a.Author = it.Authors[0].Name
	}

	if it.PublishedParsed != nil {
		a.PublishedAt = it.PublishedParsed.Unix()
	} else if it.UpdatedParsed != nil {
		a.PublishedAt = it.UpdatedParsed.Unix()
	} else if t, err := tryParseTime(it.Published); err == nil {
		a.PublishedAt = t.Unix()
	}

	// Image: prefer enclosure, then itunes:image, then first <img> in HTML.
	switch {
	case it.Image != nil && it.Image.URL != "":
		a.ImageURL = it.Image.URL
	case len(it.Enclosures) > 0 && strings.HasPrefix(strings.ToLower(it.Enclosures[0].Type), "image"):
		a.ImageURL = it.Enclosures[0].URL
	default:
		a.ImageURL = firstImageInHTML(a.ContentHTML)
	}
	a.ImageURL = resolveLink(base, a.ImageURL)

	// Tags: keep up to 3 of gofeed's per-item categories, comma-joined.
	if len(it.Categories) > 0 {
		n := len(it.Categories)
		if n > 3 {
			n = 3
		}
		parts := make([]string, 0, n)
		for _, c := range it.Categories[:n] {
			c = strings.TrimSpace(c)
			if c != "" {
				parts = append(parts, c)
			}
		}
		a.Tags = strings.Join(parts, ", ")
	}

	a.ContentHash = ContentHash(a.URL, a.Title, a.ContentText)
	return a
}

// ContentHash returns a stable SHA-256 hex digest of the article's identifying
// content. Same inputs always produce the same hash.
func ContentHash(url, title, contentText string) string {
	h := sha256.New()
	h.Write([]byte(strings.TrimSpace(url)))
	h.Write([]byte{0})
	h.Write([]byte(strings.TrimSpace(title)))
	h.Write([]byte{0})
	h.Write([]byte(strings.TrimSpace(contentText)))
	return hex.EncodeToString(h.Sum(nil))
}

func resolveLink(base *url.URL, ref string) string {
	if ref == "" {
		return ""
	}
	if base == nil {
		return ref
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return base.ResolveReference(u).String()
}

// htmlToText returns a plain-text representation of an HTML fragment by
// extracting text nodes only.
func htmlToText(s string) string {
	if s == "" {
		return ""
	}
	doc, err := html.Parse(bytes.NewReader([]byte("<root>" + s + "</root>")))
	if err != nil {
		return collapseWS(s)
	}
	var buf bytes.Buffer
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			buf.WriteString(n.Data)
			buf.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return collapseWS(buf.String())
}

var wsRe = regexp.MustCompile(`\s+`)

func collapseWS(s string) string {
	return strings.TrimSpace(wsRe.ReplaceAllString(s, " "))
}

func firstImageInHTML(s string) string {
	if s == "" {
		return ""
	}
	doc, err := html.Parse(bytes.NewReader([]byte("<root>" + s + "</root>")))
	if err != nil {
		return ""
	}
	var found string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != "" {
			return
		}
		if n.Type == html.ElementNode && n.Data == "img" {
			for _, a := range n.Attr {
				if strings.EqualFold(a.Key, "src") {
					found = a.Val
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return found
}

var timeFormats = []string{
	time.RFC3339,
	time.RFC1123Z,
	time.RFC1123,
	"2006-01-02T15:04:05Z",
	"2006-01-02 15:04:05",
}

func tryParseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, errors.New("empty")
	}
	for _, f := range timeFormats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("no format matched")
}
