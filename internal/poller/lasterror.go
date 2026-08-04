package poller

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"

	"github.com/brandonhon/ember/internal/urlcheck"
)

// statusRE pulls the HTTP status out of the fetcher's "…: status 404" error.
// The publisher's own response code is safe to surface and is the single most
// useful thing a subscriber can be told, so it survives sanitization.
var statusRE = regexp.MustCompile(`\bstatus (\d{3})\b`)

// publicFetchError turns a raw fetch/parse error into text that is safe to
// store in feeds.last_error, which every subscriber of that feed can read via
// GET /api/feeds.
//
// The raw error is not safe to expose. Go's transport errors embed whatever
// the connection actually resolved to — "dial tcp 10.0.0.5:443: connect:
// connection refused" hands any subscriber a piece of the server's internal
// network map, and redirect chains can add internal hostnames the user never
// subscribed to. The full error is still written to the log at the call site,
// so operators lose nothing.
//
// The mapping is deliberately fail-CLOSED: anything not positively recognised
// collapses to a generic message rather than falling through to err.Error().
// A new error shape from a dependency therefore cannot silently start leaking.
func publicFetchError(err error) string {
	if err == nil {
		return ""
	}

	// Ember's own SSRF guard. Worth naming precisely — it means the operator
	// must opt in via EMBER_ALLOW_PRIVATE_URLS, not that the feed is broken.
	switch {
	case errors.Is(err, urlcheck.ErrPrivate):
		return "blocked: this address is private or loopback"
	case errors.Is(err, urlcheck.ErrScheme):
		return "blocked: only http and https feeds are supported"
	case errors.Is(err, urlcheck.ErrPort):
		return "blocked: that port is not allowed"
	}

	// The publisher's HTTP status, when the fetcher recorded one.
	if m := statusRE.FindStringSubmatch(err.Error()); m != nil {
		return fmt.Sprintf("the server responded %s", m[1])
	}

	// Timeouts, including the request context deadline.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "the server took too long to respond"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "the server took too long to respond"
	}

	// DNS. The name is the feed's own hostname, which the subscriber already
	// knows, but the resolver detail behind it is not theirs to see.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "could not find that domain"
	}

	// Any remaining transport-level failure: connection refused/reset, TLS
	// handshake and certificate problems, unreachable networks. These are the
	// ones that carry resolved IPs, so they get a category and nothing more.
	var urlErr *url.Error
	var opErr *net.OpError
	if errors.As(err, &opErr) || errors.As(err, &urlErr) {
		if strings.Contains(strings.ToLower(err.Error()), "certificate") ||
			strings.Contains(strings.ToLower(err.Error()), "tls") {
			return "the server's TLS certificate could not be verified"
		}
		return "could not connect to the server"
	}

	// Redirect-loop guard from the fetcher.
	if strings.Contains(err.Error(), "stopped after") && strings.Contains(err.Error(), "redirects") {
		return "too many redirects"
	}

	// Parse failures: the fetch worked, the body was not a usable feed. The
	// gofeed/XML detail can quote raw document content, so it is not repeated.
	if strings.Contains(err.Error(), "feed: empty body") {
		return "the server returned an empty response"
	}
	if strings.HasPrefix(err.Error(), "feed: parse") || strings.Contains(err.Error(), "Failed to detect feed type") {
		return "the response was not a valid RSS or Atom feed"
	}

	// Unrecognised. Say nothing specific rather than risk passing detail
	// through — the log line at the call site has the real error.
	return "the feed could not be fetched"
}
