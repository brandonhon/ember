package feed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultUserAgent identifies ember to upstream servers.
const DefaultUserAgent = "ember/0.x (+https://github.com/brandonhon/ember)"

// FetchResult is the outcome of a single feed fetch.
type FetchResult struct {
	// Changed is false when the server returned 304 Not Modified.
	Changed      bool
	Body         []byte
	ETag         string
	LastModified string
	StatusCode   int
	ContentType  string
}

// Fetcher performs HTTP fetches with conditional GET semantics.
type Fetcher struct {
	Client    *http.Client
	UserAgent string
}

// NewFetcher returns a Fetcher with sensible defaults.
func NewFetcher(timeout time.Duration) *Fetcher {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Fetcher{
		Client:    &http.Client{Timeout: timeout},
		UserAgent: DefaultUserAgent,
	}
}

// Fetch fetches the feed URL, sending If-None-Match / If-Modified-Since when
// the caller provides the values from a previous fetch. A 304 yields
// Changed=false and an empty body.
func (f *Fetcher) Fetch(ctx context.Context, target, etag, lastModified string) (FetchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return FetchResult{}, err
	}
	req.Header.Set("User-Agent", f.userAgent())
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml;q=0.9, */*;q=0.5")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}

	resp, err := f.Client.Do(req)
	if err != nil {
		return FetchResult{}, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotModified:
		return FetchResult{
			Changed:      false,
			StatusCode:   resp.StatusCode,
			ETag:         resp.Header.Get("ETag"),
			LastModified: resp.Header.Get("Last-Modified"),
		}, nil
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		if err != nil {
			return FetchResult{}, err
		}
		return FetchResult{
			Changed:      true,
			Body:         body,
			StatusCode:   resp.StatusCode,
			ETag:         resp.Header.Get("ETag"),
			LastModified: resp.Header.Get("Last-Modified"),
			ContentType:  resp.Header.Get("Content-Type"),
		}, nil
	default:
		return FetchResult{StatusCode: resp.StatusCode}, fmt.Errorf("feed: fetch %s: status %d", target, resp.StatusCode)
	}
}

func (f *Fetcher) userAgent() string {
	if f.UserAgent != "" {
		return f.UserAgent
	}
	return DefaultUserAgent
}

// IsTransientError returns true for errors a caller should back off on rather
// than disable a feed (network blips, timeouts, 5xx).
func IsTransientError(err error) bool {
	if err == nil {
		return false
	}
	var ne interface{ Timeout() bool }
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return true // we treat all fetch errors as transient by default
}
