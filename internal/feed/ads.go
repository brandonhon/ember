package feed

import (
	"bytes"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// Curated, per-publisher in-body ad removal. We only touch source hosts we've
// vetted (publisherAdHosts) so feeds we haven't reviewed are never altered.
// CSS classes are already gone by the time the body is sanitized, so ad blocks
// are matched by their asset/link URLs instead.
//
// BleepingComputer ships sponsored banners and end-of-article promos whose
// images live under bleepstatic.com/c/ (editorial images use /content/) and
// whose CTAs point at hubs.li. We find those nodes and drop the smallest
// ad-sized container around each.
var (
	publisherAdHosts = map[string]bool{
		"bleepingcomputer.com":     true,
		"www.bleepingcomputer.com": true,
	}
	adImageMarker = "bleepstatic.com/c/"
	adLinkHosts   = map[string]bool{"hubs.li": true}
)

// adContainerMaxText caps how much text an ancestor may hold to still be
// considered "an ad block" rather than article body — the sponsored divs are a
// couple hundred chars; the article wrapper is thousands.
const adContainerMaxText = 600

// StripPublisherAds removes known sponsored blocks from an article body for
// curated publishers. No-op (returns input unchanged) for any other host or on
// parse failure.
func StripPublisherAds(body, sourceURL string) string {
	if body == "" {
		return body
	}
	u, err := url.Parse(sourceURL)
	if err != nil || !publisherAdHosts[strings.ToLower(u.Host)] {
		return body
	}
	doc, err := html.Parse(bytes.NewReader([]byte("<root>" + body + "</root>")))
	if err != nil {
		return body
	}
	root := findElement(doc, "root")
	if root == nil {
		return body
	}

	targets := map[*html.Node]bool{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && isAdNode(n) {
			if t := adRemovalTarget(n, root); t != nil {
				targets[t] = true
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if len(targets) == 0 {
		return body
	}
	for t := range targets {
		if t.Parent != nil {
			t.Parent.RemoveChild(t)
		}
	}
	return renderChildren(root)
}

func isAdNode(n *html.Node) bool {
	switch n.Data {
	case "img":
		return strings.Contains(strings.ToLower(attr(n, "src")), adImageMarker)
	case "a":
		if u, err := url.Parse(attr(n, "href")); err == nil {
			return adLinkHosts[strings.ToLower(u.Host)]
		}
	}
	return false
}

// adRemovalTarget walks up from an ad node and returns what to delete: the
// nearest ad-sized <div> wrapper (the end-of-article sponsored unit), else the
// nearest <p> (an inline banner), else the node itself.
func adRemovalTarget(n, root *html.Node) *html.Node {
	var nearestP *html.Node
	for a := n.Parent; a != nil && a != root; a = a.Parent {
		if a.Type != html.ElementNode {
			continue
		}
		if a.Data == "div" && textLen(a) < adContainerMaxText {
			return a
		}
		if a.Data == "p" && nearestP == nil {
			nearestP = a
		}
	}
	if nearestP != nil {
		return nearestP
	}
	return n
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func textLen(n *html.Node) int {
	total := 0
	var f func(*html.Node)
	f = func(x *html.Node) {
		if x.Type == html.TextNode {
			total += len(strings.TrimSpace(x.Data))
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return total
}

func findElement(doc *html.Node, name string) *html.Node {
	var found *html.Node
	var f func(*html.Node)
	f = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == name {
			found = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)
	return found
}

func renderChildren(root *html.Node) string {
	var b bytes.Buffer
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&b, c); err != nil {
			return b.String()
		}
	}
	return b.String()
}
