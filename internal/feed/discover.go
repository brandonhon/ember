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
// validate is required and is called against every URL Discover is about to
// fetch (the starting URL and each probe). Pass an SSRF guard such as
// internal/urlcheck.Check; if validate returns an error, the request is
// skipped. Without it Discover would be a request-forgery primitive — the
// caller would need to wrap every transport / redirect / probe themselves.
//
// Returns ErrNoFeed if nothing is discovered.
func Discover(ctx context.Context, c *http.Client, target string, validate func(rawURL string) error) (string, error) {
	if c == nil {
		c = http.DefaultClient
	}
	if validate == nil {
		return "", errors.New("feed: Discover requires a non-nil validate function")
	}
	if err := validate(target); err != nil {
		return "", fmt.Errorf("feed: validate target: %w", err)
	}
	// Pre-pass: recognize known URL shapes (YouTube channel/playlist/handle,
	// Mastodon profile) and rewrite them straight to their feed URL. For
	// shapes that need a network hop (YouTube /@handle), this runs through
	// the same validate guard. Returns the original target on no-match so
	// the rest of the function still runs as before.
	if rewritten, ok, err := RewriteKnown(ctx, c, target, validate); err != nil {
		return "", err
	} else if ok {
		// Validate the rewritten URL too — it crosses the same trust boundary.
		if err := validate(rewritten); err != nil {
			return "", fmt.Errorf("feed: validate rewritten: %w", err)
		}
		target = rewritten
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
		if abs, rerr := resolveRef(parsedTarget, href); rerr == nil && abs != "" {
			// The discovered link crosses the same trust boundary as the
			// target; validate before returning it (the caller fetches it).
			// On reject/unresolvable, fall through to the fallback probes —
			// matching DiscoverAll's drop-and-continue rather than handing back
			// an unchecked URL.
			if verr := validate(abs); verr == nil {
				return abs, nil
			}
		}
	}

	for _, p := range DiscoveryFallbacks {
		probe := *parsedTarget
		probe.Path = p
		probe.RawQuery = ""
		probe.Fragment = ""
		ok, err := probeFeed(ctx, c, probe.String(), validate)
		if err == nil && ok {
			return probe.String(), nil
		}
	}
	return "", ErrNoFeed
}

// Discovered is a single feed surfaced by DiscoverAll.
type Discovered struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Type  string `json:"type"` // "rss", "atom", or "" when unknown
}

// DiscoverAll is like Discover but returns every feed advertised by an HTML
// page rather than only the first. It is used by the add-feed UI to show a
// picker when a site exposes multiple feeds.
//
//  1. GET the URL. If it is itself a feed, return that single entry.
//  2. Otherwise collect every <link rel="alternate" type=".../(rss|atom)">.
//  3. If the page advertised none, probe DiscoveryFallbacks and return any
//     that respond as a feed.
//
// validate is required and is called against the target and every discovered
// or probed URL — the same SSRF discipline as Discover. Discovered URLs that
// fail validation are dropped. Results are de-duplicated by URL. Returns an
// empty slice (nil error) when the page loads but advertises no feed.
func DiscoverAll(ctx context.Context, c *http.Client, target string, validate func(rawURL string) error) ([]Discovered, error) {
	if c == nil {
		c = http.DefaultClient
	}
	if validate == nil {
		return nil, errors.New("feed: DiscoverAll requires a non-nil validate function")
	}
	if err := validate(target); err != nil {
		return nil, fmt.Errorf("feed: validate target: %w", err)
	}
	parsedTarget, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("feed: parse target: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", DefaultUserAgent)
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); isFeedContentType(ct) {
		return []Discovered{{URL: target, Type: feedTypeFromHint(ct)}}, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	var out []Discovered
	for _, alt := range findAllAlternatesInHTML(body) {
		abs, rerr := resolveRef(parsedTarget, alt.href)
		if rerr != nil || abs == "" {
			continue
		}
		if err := validate(abs); err != nil {
			continue // drop SSRF-rejected feed links
		}
		if _, dup := seen[abs]; dup {
			continue
		}
		seen[abs] = struct{}{}
		out = append(out, Discovered{URL: abs, Title: strings.TrimSpace(alt.title), Type: feedTypeFromHint(alt.typ)})
	}
	if len(out) > 0 {
		return out, nil
	}

	// No <link> hints — probe common paths, collecting any that respond.
	for _, p := range DiscoveryFallbacks {
		probe := *parsedTarget
		probe.Path = p
		probe.RawQuery = ""
		probe.Fragment = ""
		ps := probe.String()
		if _, dup := seen[ps]; dup {
			continue
		}
		if ok, perr := probeFeed(ctx, c, ps, validate); perr == nil && ok {
			seen[ps] = struct{}{}
			out = append(out, Discovered{URL: ps})
		}
	}
	return out, nil
}

// feedTypeFromHint maps a Content-Type or <link type> hint to "rss"/"atom"/"".
func feedTypeFromHint(hint string) string {
	hint = strings.ToLower(hint)
	switch {
	case strings.Contains(hint, "atom"):
		return "atom"
	case strings.Contains(hint, "rss"):
		return "rss"
	default:
		return ""
	}
}

type altLink struct {
	href, title, typ string
}

// findAllAlternatesInHTML returns every <link rel="alternate"> RSS/Atom feed
// declared in the document, in document order.
func findAllAlternatesInHTML(body []byte) []altLink {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil
	}
	var out []altLink
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "link" {
			var rel, typ, href, title string
			for _, a := range n.Attr {
				switch strings.ToLower(a.Key) {
				case "rel":
					rel = strings.ToLower(a.Val)
				case "type":
					typ = strings.ToLower(a.Val)
				case "href":
					href = a.Val
				case "title":
					title = a.Val
				}
			}
			if rel == "alternate" && (strings.Contains(typ, "rss") || strings.Contains(typ, "atom")) && href != "" {
				out = append(out, altLink{href: href, title: title, typ: typ})
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return out
}

func isFeedContentType(ct string) bool {
	ct = strings.ToLower(ct)
	return strings.Contains(ct, "application/rss") ||
		strings.Contains(ct, "application/atom") ||
		strings.Contains(ct, "application/feed+json")
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

func probeFeed(ctx context.Context, c *http.Client, target string, validate func(rawURL string) error) (bool, error) {
	if err := validate(target); err != nil {
		return false, nil
	}
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
