package feed

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-shiori/go-readability"
)

// Readable holds the extracted full-content view of an article.
type Readable struct {
	Title    string
	HTML     string
	Text     string
	ImageURL string
}

// ExtractFromURL fetches the URL with the given client and returns the
// readability-extracted view.
func ExtractFromURL(ctx context.Context, c *http.Client, target string) (Readable, error) {
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return Readable{}, err
	}
	req.Header.Set("User-Agent", DefaultUserAgent)
	resp, err := c.Do(req)
	if err != nil {
		return Readable{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Readable{}, errors.New("readability: non-2xx status")
	}
	u, _ := url.Parse(target)
	art, err := readability.FromReader(resp.Body, u)
	if err != nil {
		return Readable{}, err
	}
	return Readable{
		Title:    strings.TrimSpace(art.Title),
		HTML:     art.Content,
		Text:     strings.TrimSpace(art.TextContent),
		ImageURL: art.Image,
	}, nil
}

// extractFromHTML runs readability over the given HTML body without making an
// HTTP request. Internal test helper; not part of the package's public API.
func extractFromHTML(body, target string) (Readable, error) {
	u, _ := url.Parse(target)
	art, err := readability.FromReader(strings.NewReader(body), u)
	if err != nil {
		return Readable{}, err
	}
	return Readable{
		Title:    strings.TrimSpace(art.Title),
		HTML:     art.Content,
		Text:     strings.TrimSpace(art.TextContent),
		ImageURL: art.Image,
	}, nil
}
