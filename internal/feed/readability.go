package feed

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-shiori/go-readability"
)

// maxReadableBytes caps the page body fed to the readability parser. Unlike the
// feed fetcher this path had no limit, so a hostile article URL (reached via the
// enrichment path) could stream an unbounded body — or a gzip bomb the HTTP
// stack transparently inflates — into a full DOM and OOM the process.
const maxReadableBytes = 8 << 20 // 8 MiB

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
		// Require a caller-supplied client: the SSRF guard (redirect + dial)
		// lives on it, so a nil client would be an unguarded fetch. Callers
		// build a guarded client (see poller.enrichWithReadability).
		return Readable{}, errors.New("readability: nil http client (SSRF guard required)")
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
	art, err := readability.FromReader(io.LimitReader(resp.Body, maxReadableBytes), u)
	if err != nil {
		return Readable{}, err
	}
	return readableFrom(art), nil
}

// readableFrom maps a go-readability article onto our view, applying the
// sanitizer and the image-URL guard. Both extraction entry points funnel
// through here so neither can return unsanitized HTML.
func readableFrom(art readability.Article) Readable {
	return Readable{
		Title:    strings.TrimSpace(art.Title),
		HTML:     SanitizeHTML(art.Content),
		Text:     strings.TrimSpace(art.TextContent),
		ImageURL: SafeImageURL(art.Image),
	}
}
