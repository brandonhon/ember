// Package urlcheck validates outbound URLs to block SSRF attempts. Used by
// every code path that fetches a user-supplied URL: add-feed, OPML import,
// readability enrichment, and admin branding favicon writes.
//
// The check enforces two policies:
//   - Scheme allowlist: only http and https.
//   - Private-network block: any resolved address inside RFC1918, loopback,
//     link-local, CGNAT, or IPv6 ULA / link-local ranges is rejected.
//
// Both policies can be relaxed with EMBER_ALLOW_PRIVATE_URLS=1 for homelab
// users who want to subscribe to feeds on their LAN. Off by default.
package urlcheck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ErrPrivate is returned when the URL resolves to a private/loopback/CGNAT
// address and EMBER_ALLOW_PRIVATE_URLS is not set.
var ErrPrivate = errors.New("urlcheck: URL resolves to a private or loopback address")

// ErrScheme is returned for non-http(s) URLs.
var ErrScheme = errors.New("urlcheck: only http and https URLs are allowed")

// privateBlocks are the CIDRs we refuse to make outbound requests against.
// Pulled from RFC1918, RFC4193, and common metadata/loopback ranges.
var privateBlocks = mustParseCIDRs(
	"10.0.0.0/8",        // RFC1918
	"172.16.0.0/12",     // RFC1918
	"192.168.0.0/16",    // RFC1918
	"127.0.0.0/8",       // loopback
	"169.254.0.0/16",    // link-local + AWS/GCP metadata
	"100.64.0.0/10",     // CGNAT
	"0.0.0.0/8",         // "this network"
	"::1/128",           // loopback IPv6
	"fc00::/7",          // unique-local IPv6
	"fe80::/10",         // link-local IPv6
)

// Resolver lets tests inject a fake DNS lookup. Defaults to net.LookupIP.
type Resolver func(ctx context.Context, host string) ([]net.IP, error)

func defaultResolver(ctx context.Context, host string) ([]net.IP, error) {
	r := &net.Resolver{}
	addrs, err := r.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.IP)
	}
	return out, nil
}

// Check returns nil when the URL is safe to fetch. When allowPrivate is true
// the private-IP check is skipped (scheme allowlist still enforced).
func Check(ctx context.Context, raw string, allowPrivate bool) error {
	return CheckWith(ctx, raw, allowPrivate, defaultResolver)
}

// CheckWith is Check with a pluggable resolver for tests.
func CheckWith(ctx context.Context, raw string, allowPrivate bool, resolve Resolver) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("urlcheck: parse: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: got %q", ErrScheme, u.Scheme)
	}
	if u.Host == "" {
		return errors.New("urlcheck: missing host")
	}
	if allowPrivate {
		return nil
	}
	host := u.Hostname()
	// If the host is already a literal IP, check it directly without DNS.
	if ip := net.ParseIP(host); ip != nil {
		if isPrivate(ip) {
			return fmt.Errorf("%w: %s", ErrPrivate, ip)
		}
		return nil
	}
	ips, err := resolve(ctx, host)
	if err != nil {
		return fmt.Errorf("urlcheck: resolve %s: %w", host, err)
	}
	for _, ip := range ips {
		if isPrivate(ip) {
			return fmt.Errorf("%w: %s -> %s", ErrPrivate, host, ip)
		}
	}
	return nil
}

func isPrivate(ip net.IP) bool {
	for _, b := range privateBlocks {
		if b.Contains(ip) {
			return true
		}
	}
	return false
}

func mustParseCIDRs(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic("urlcheck: bad CIDR " + c + ": " + err.Error())
		}
		out = append(out, n)
	}
	return out
}
