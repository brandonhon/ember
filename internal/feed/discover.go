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

// requireDiscoveryDeps checks the dependencies both entry points need. The
// target itself is validated by fetchDiscoveryPage, which is the function that
// actually reaches the network — keeping validation next to the fetch means it
// happens exactly once, on the URL that is really requested.
func requireDiscoveryDeps(name string, c *http.Client, validate func(string) error) error {
	if c == nil {
		return errors.New("feed: " + name + " requires a non-nil http.Client")
	}
	if validate == nil {
		return errors.New("feed: " + name + " requires a non-nil validate function")
	}
	return nil
}

// fetchedPage is the result of loading a discovery target.
type fetchedPage struct {
	parsed      *url.URL
	contentType string
	// body is the bounded HTML body; empty when isFeed is true (the response
	// was the feed itself, so there is nothing to scrape).
	body   []byte
	isFeed bool
}

// fetchDiscoveryPage validates the target, GETs it, and reads a bounded body.
//
// It re-runs validate itself rather than trusting the caller to have done so.
// Discovery is a user-initiated action (add-feed), not a hot path, so the extra
// check is cheap — and it makes fetching an unvalidated URL structurally
// impossible instead of a comment someone has to notice. A previous version of
// this package leaked exactly that way: the <link rel="alternate"> branch
// returned a URL the caller then fetched without a check.
func fetchDiscoveryPage(ctx context.Context, c *http.Client, target string, validate func(string) error) (fetchedPage, error) {
	if validate == nil {
		return fetchedPage{}, errors.New("feed: fetchDiscoveryPage requires a non-nil validate function")
	}
	if err := validate(target); err != nil {
		return fetchedPage{}, fmt.Errorf("feed: validate target: %w", err)
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return fetchedPage{}, fmt.Errorf("feed: parse target: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fetchedPage{}, err
	}
	req.Header.Set("User-Agent", DefaultUserAgent)
	resp, err := c.Do(req)
	if err != nil {
		return fetchedPage{}, err
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if isFeedContentType(ct) {
		return fetchedPage{parsed: parsed, contentType: ct, isFeed: true}, nil
	}
	// Cap the HTML we scrape: a hostile origin could otherwise stream an
	// unbounded body into memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDiscoveryBodyBytes))
	if err != nil {
		return fetchedPage{}, err
	}
	return fetchedPage{parsed: parsed, contentType: ct, body: body}, nil
}

// maxDiscoveryBodyBytes bounds the HTML scraped for <link rel="alternate">.
const maxDiscoveryBodyBytes = 1 << 20

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
	if err := requireDiscoveryDeps("Discover", c, validate); err != nil {
		return "", err
	}
	// Pre-pass: recognize known URL shapes (YouTube channel/playlist/handle,
	// Mastodon profile) and rewrite them straight to their feed URL. For
	// shapes that need a network hop (YouTube /@handle), this runs through
	// the same validate guard. Returns the original target on no-match so
	// the rest of the function still runs as before.
	if rewritten, ok, err := RewriteKnown(ctx, c, target, validate); err != nil {
		return "", err
	} else if ok {
		// No separate validate here: fetchDiscoveryPage validates whatever it
		// is about to request, so the rewritten URL is checked on the way in.
		target = rewritten
	}
	page, err := fetchDiscoveryPage(ctx, c, target, validate)
	if err != nil {
		return "", err
	}
	if page.isFeed {
		return target, nil
	}
	parsedTarget := page.parsed

	if href := findAlternateInHTML(page.body); href != "" {
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
	if err := requireDiscoveryDeps("DiscoverAll", c, validate); err != nil {
		return nil, err
	}
	page, err := fetchDiscoveryPage(ctx, c, target, validate)
	if err != nil {
		return nil, err
	}
	if page.isFeed {
		return []Discovered{{URL: target, Type: feedTypeFromHint(page.contentType)}}, nil
	}
	parsedTarget := page.parsed

	seen := make(map[string]struct{})
	var out []Discovered
	for _, alt := range findAllAlternatesInHTML(page.body) {
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
	return strings.Contains(snippet, "<rss") || strings.Contains(snippet, "<feed") || strings.Contains(snippet, "<rdf:rdf"), nil
}
