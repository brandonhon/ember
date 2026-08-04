package poller

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/brandonhon/ember/internal/urlcheck"
)

func TestPublicFetchError_Classification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"ssrf private", fmt.Errorf("fetch: %w", urlcheck.ErrPrivate), "blocked: this address is private or loopback"},
		{"ssrf scheme", fmt.Errorf("fetch: %w", urlcheck.ErrScheme), "blocked: only http and https feeds are supported"},
		{"ssrf port", fmt.Errorf("fetch: %w", urlcheck.ErrPort), "blocked: that port is not allowed"},
		{"http status", errors.New(`feed: fetch https://x.test/rss: status 404`), "the server responded 404"},
		{"http 500", errors.New(`feed: fetch https://x.test/rss: status 503`), "the server responded 503"},
		{"deadline", fmt.Errorf("get: %w", context.DeadlineExceeded), "the server took too long to respond"},
		{"dns", &url.Error{Op: "Get", URL: "https://x.test/rss",
			Err: &net.DNSError{Err: "no such host", Name: "x.test"}}, "could not find that domain"},
		{"empty body", errors.New("feed: empty body"), "the server returned an empty response"},
		{"redirect loop", errors.New("stopped after 10 redirects"), "too many redirects"},
		{"unknown", errors.New("something nobody anticipated"), "the feed could not be fetched"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := publicFetchError(tc.err); got != tc.want {
				t.Errorf("publicFetchError(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// The point of the sanitizer is that a resolved address never reaches a
// subscriber. Drive the real error shapes Go produces for connection failures
// and assert nothing address-like survives.
func TestPublicFetchError_NeverLeaksAddresses(t *testing.T) {
	ipish := regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b|\[[0-9a-fA-F:]+\]`)

	errs := []error{
		&url.Error{Op: "Get", URL: "https://feed.test/rss", Err: &net.OpError{
			Op: "dial", Net: "tcp",
			Addr: &net.TCPAddr{IP: net.ParseIP("10.0.0.5"), Port: 443},
			Err:  errors.New("connect: connection refused"),
		}},
		&url.Error{Op: "Get", URL: "https://feed.test/rss", Err: &net.OpError{
			Op: "read", Net: "tcp",
			Addr: &net.TCPAddr{IP: net.ParseIP("192.168.1.20"), Port: 80},
			Err:  errors.New("read: connection reset by peer"),
		}},
		// NOTE: only fixtures that actually contain an address belong here —
		// the guard below fails the test otherwise, so a fixture without one
		// would make this prove nothing. TLS errors are covered separately.
		fmt.Errorf("proxy dial tcp 172.16.9.9:3128: %w", errors.New("i/o timeout")),
	}

	for _, err := range errs {
		got := publicFetchError(err)
		if got == "" {
			t.Errorf("no message produced for %v", err)
		}
		// Sanity: the raw error really does contain an address, so this test
		// would be vacuous if the sanitizer were removed.
		if !ipish.MatchString(err.Error()) {
			t.Fatalf("fixture %q has no address in it — test would prove nothing", err)
		}
		if ipish.MatchString(got) {
			t.Errorf("sanitized message leaked an address: %q (from %v)", got, err)
		}
		if strings.Contains(got, "10.0.0.5") || strings.Contains(got, "192.168") {
			t.Errorf("sanitized message leaked a private address: %q", got)
		}
	}
}

// A TLS problem is worth distinguishing from a plain connection failure — it
// tells the user the feed is reachable but untrusted.
func TestPublicFetchError_TLSIsDistinct(t *testing.T) {
	tlsErr := &url.Error{Op: "Get", URL: "https://feed.test/rss",
		Err: errors.New("tls: failed to verify certificate: x509: certificate has expired")}
	if got := publicFetchError(tlsErr); got != "the server's TLS certificate could not be verified" {
		t.Errorf("TLS error = %q, want the TLS-specific message", got)
	}
	connErr := &url.Error{Op: "Get", URL: "https://feed.test/rss", Err: &net.OpError{
		Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}}
	if got := publicFetchError(connErr); got != "could not connect to the server" {
		t.Errorf("connection error = %q, want the generic connect message", got)
	}
}
