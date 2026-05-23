package feed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// ErrNoFeed is returned when no feed could be discovered.
var ErrNoFeed = errors.New("feed: no feed link found")

// DiscoveryFallbacks are common feed paths tried when an HTML page does not
// expose a <link rel="alternate"> hint.
var DiscoveryFallbacks = []string{"/feed", "/rss", "/atom.xml", "/feed.xml", "/index.xml"}

// Discover attempts to find a feed URL given an HTTP client and a starting
// URL. It does the following, in order:
//  1. GET the URL. If the response Content-Type indicates feed, return the URL.
//  2. Parse the HTML body for <link rel="alternate" type="application/(rss|atom)+xml">.
//  3. Probe each entry in DiscoveryFallbacks at the same origin and return the
//     first that responds 2xx with a feed-shaped content type.
//
// Returns ErrNoFeed if nothing is discovered.
func Discover(ctx context.Context, c *http.Client, target string) (string, error) {
	if c == nil {
		c = http.DefaultClient
	}
	parsedTarget, err := url.Parse(target)
	if err != nil {
		return "", fmt.Errorf("feed: parse target: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", DefaultUserAgent)
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if isFeedContentType(ct) {
		return target, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	if href := findAlternateInHTML(body); href != "" {
		abs, _ := resolveRef(parsedTarget, href)
		return abs, nil
	}

	for _, p := range DiscoveryFallbacks {
		probe := *parsedTarget
		probe.Path = p
		probe.RawQuery = ""
		probe.Fragment = ""
		ok, err := probeFeed(ctx, c, probe.String())
		if err == nil && ok {
			return probe.String(), nil
		}
	}
	return "", ErrNoFeed
}

func isFeedContentType(ct string) bool {
	ct = strings.ToLower(ct)
	return strings.Contains(ct, "application/rss") ||
		strings.Contains(ct, "application/atom") ||
		strings.Contains(ct, "application/feed+json") ||
		strings.Contains(ct, "application/xml") ||
		strings.Contains(ct, "text/xml")
}

func findAlternateInHTML(body []byte) string {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return ""
	}
	var found string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != "" {
			return
		}
		if n.Type == html.ElementNode && n.Data == "link" {
			var rel, typ, href string
			for _, a := range n.Attr {
				switch strings.ToLower(a.Key) {
				case "rel":
					rel = strings.ToLower(a.Val)
				case "type":
					typ = strings.ToLower(a.Val)
				case "href":
					href = a.Val
				}
			}
			if rel == "alternate" && (strings.Contains(typ, "rss") || strings.Contains(typ, "atom")) && href != "" {
				found = href
				return
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return found
}

func resolveRef(base *url.URL, ref string) (string, error) {
	u, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(u).String(), nil
}

func probeFeed(ctx context.Context, c *http.Client, target string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", DefaultUserAgent)
	resp, err := c.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, nil
	}
	if isFeedContentType(resp.Header.Get("Content-Type")) {
		return true, nil
	}
	// Sniff a few bytes for an XML/feed root.
	buf := make([]byte, 256)
	n, _ := io.ReadFull(resp.Body, buf)
	snippet := strings.ToLower(string(buf[:n]))
	return strings.Contains(snippet, "<rss") || strings.Contains(snippet, "<feed") || strings.Contains(snippet, "<?xml"), nil
}
