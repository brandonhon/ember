package urlcheck

import (
	"context"
	"errors"
	"net"
	"testing"
)

func fakeResolver(byHost map[string][]string) Resolver {
	return func(_ context.Context, host string) ([]net.IP, error) {
		ipsStr, ok := byHost[host]
		if !ok {
			return nil, errors.New("not in fake")
		}
		out := make([]net.IP, 0, len(ipsStr))
		for _, s := range ipsStr {
			ip := net.ParseIP(s)
			if ip == nil {
				return nil, errors.New("bad ip in fake: " + s)
			}
			out = append(out, ip)
		}
		return out, nil
	}
}

func TestCheck_Scheme(t *testing.T) {
	cases := []struct{ url string }{
		{"ftp://example.com/feed"},
		{"file:///etc/passwd"},
		{"gopher://example.com"},
		{"javascript:alert(1)"},
	}
	for _, c := range cases {
		err := CheckWith(context.Background(), c.url, false, fakeResolver(nil))
		if !errors.Is(err, ErrScheme) {
			t.Errorf("%s: expected ErrScheme, got %v", c.url, err)
		}
	}
}

func TestCheck_BlockedPorts(t *testing.T) {
	resolver := fakeResolver(map[string][]string{"example.com": {"93.184.216.34"}})
	for _, u := range []string{
		"https://example.com:22/",
		"http://example.com:25/feed",
		"https://example.com:6379/",
		"http://example.com:3306/",
		"https://example.com:11211/",
	} {
		if err := CheckWith(context.Background(), u, false, resolver); !errors.Is(err, ErrPort) {
			t.Errorf("%s: expected ErrPort, got %v", u, err)
		}
	}
	// Blocked even when private URLs are allowed (a service port is never a feed).
	if err := CheckWith(context.Background(), "https://example.com:22/", true, fakeResolver(nil)); !errors.Is(err, ErrPort) {
		t.Errorf("allowPrivate should still block port 22, got %v", err)
	}
	// Web ports pass through to the address check.
	for _, u := range []string{"https://example.com/", "https://example.com:443/", "http://example.com:8080/feed"} {
		if err := CheckWith(context.Background(), u, false, resolver); err != nil {
			t.Errorf("%s: expected ok, got %v", u, err)
		}
	}
}

func TestCheck_PrivateIPLiteral(t *testing.T) {
	priv := []string{
		"http://127.0.0.1/",
		"http://10.0.0.5/",
		"http://192.168.1.1/",
		"http://172.16.5.5/",
		"http://169.254.169.254/",
		"http://[::1]/",
		"http://0.0.0.0/",
		"http://100.64.5.5/",
		"http://[2002:c0a8:0101::]/",   // 6to4 encoding 192.168.1.1
		"http://[64:ff9b::a9fe:a9fe]/", // NAT64 mapping 169.254.169.254 (metadata)
	}
	for _, u := range priv {
		err := CheckWith(context.Background(), u, false, fakeResolver(nil))
		if !errors.Is(err, ErrPrivate) {
			t.Errorf("%s: expected ErrPrivate, got %v", u, err)
		}
	}
}

func TestCheck_PrivateDNSResolves(t *testing.T) {
	resolver := fakeResolver(map[string][]string{
		"sneaky.example.com": {"10.0.0.5"},
	})
	err := CheckWith(context.Background(), "http://sneaky.example.com/", false, resolver)
	if !errors.Is(err, ErrPrivate) {
		t.Errorf("DNS to private: expected ErrPrivate, got %v", err)
	}
}

func TestCheck_PublicAllowed(t *testing.T) {
	resolver := fakeResolver(map[string][]string{
		"example.com": {"93.184.216.34"},
	})
	if err := CheckWith(context.Background(), "https://example.com/feed", false, resolver); err != nil {
		t.Errorf("public URL rejected: %v", err)
	}
}

func TestCheck_AllowPrivateBypass(t *testing.T) {
	err := CheckWith(context.Background(), "http://192.168.1.10/feed", true, fakeResolver(nil))
	if err != nil {
		t.Errorf("allowPrivate=true should bypass: %v", err)
	}
}

func TestDialContext_RejectsPrivateLiteral(t *testing.T) {
	dial := DialContext(false)
	// A literal private IP is rejected before any connection attempt.
	for _, addr := range []string{"127.0.0.1:80", "169.254.169.254:80", "10.0.0.5:443"} {
		if _, err := dial(context.Background(), "tcp", addr); !errors.Is(err, ErrPrivate) {
			t.Errorf("dial %s: want ErrPrivate, got %v", addr, err)
		}
	}
}
