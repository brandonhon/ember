package urlcheck

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
)

// failResolve makes DNS unavailable so these cases exercise literal-IP
// handling only — no lookup can mask a gap in the block list.
func failResolve(context.Context, string) ([]net.IP, error) {
	return nil, errors.New("dns disabled in this test")
}

// Alternate encodings of a local or internal address must all be rejected.
// The plain forms are covered by TestCheck_PrivateIPLiteral; these are the
// ones an SSRF filter typically misses. This is the shared primitive —
// internal/feed discovery, internal/push delivery, the poller's readability
// fetch and the image proxy all delegate here, so one gap is a bypass for all
// of them at once.
func TestCheck_RejectsAlternateLocalEncodings(t *testing.T) {
	for _, tc := range []struct{ raw, why string }{
		{"http://[::]/", "unspecified v6 — connects to localhost on most stacks"},
		{"http://[::ffff:127.0.0.1]/", "v4-mapped v6 loopback"},
		{"http://[::ffff:169.254.169.254]/", "v4-mapped v6 metadata"},
		{"http://[::127.0.0.1]/", "v4-compatible v6 — To4() does not normalize this form"},
		{"http://[::a9fe:a9fe]/", "v4-compatible v6 metadata, hex notation"},
		{"http://[64:ff9b::7f00:1]/", "NAT64-wrapped loopback"},
		{"http://[2002:7f00:1::]/", "6to4-wrapped loopback"},
		{"http://127.1/", "short-form loopback"},
		{"http://[fe80::1]/", "IPv6 link-local"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			if err := CheckWith(context.Background(), tc.raw, false, failResolve); err == nil {
				t.Errorf("ALLOWED %s (%s) — SSRF bypass", tc.raw, tc.why)
			}
		})
	}
}

// Public addresses, including IPv6, must survive the added blocks.
func TestCheck_PublicIPv6Allowed(t *testing.T) {
	for _, raw := range []string{
		"http://[2606:4700:4700::1111]/",
		"http://[2001:4860:4860::8888]/",
		"https://1.1.1.1/feed.xml",
	} {
		if err := CheckWith(context.Background(), raw, false, failResolve); err != nil {
			t.Errorf("blocked a public address %s: %v", raw, err)
		}
	}
}

// allowPrivate is the homelab opt-in for network location only — the scheme
// and port policies are not about location and must still apply.
func TestCheck_AllowPrivateStillEnforcesSchemeAndPort(t *testing.T) {
	ctx := context.Background()
	if err := CheckWith(ctx, "file:///etc/passwd", true, failResolve); !errors.Is(err, ErrScheme) {
		t.Errorf("allowPrivate must still reject a non-http scheme, got %v", err)
	}
	if err := CheckWith(ctx, "http://192.168.1.10:22/", true, failResolve); !errors.Is(err, ErrPort) {
		t.Errorf("allowPrivate must still reject a blocked port, got %v", err)
	}
}

// ANY private address in a multi-answer DNS response disqualifies the host —
// the dialer could pick it.
func TestCheck_MixedResolverAnswerIsRejected(t *testing.T) {
	mixed := func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34"), net.ParseIP("10.0.0.5")}, nil
	}
	if err := CheckWith(context.Background(), "http://mixed.test/feed", false, mixed); !errors.Is(err, ErrPrivate) {
		t.Errorf("a mixed answer set containing a private IP must be rejected, got %v", err)
	}
}

// The dialer is the TOCTOU backstop, so it must reject the same alternate
// encodings the pre-flight check does.
func TestDialContext_RejectsAlternateEncodings(t *testing.T) {
	dial := DialContext(false)
	for _, addr := range []string{"[::]:80", "[::ffff:127.0.0.1]:80", "[::1]:80", "[::127.0.0.1]:80"} {
		if _, err := dial(context.Background(), "tcp", addr); !errors.Is(err, ErrPrivate) {
			t.Errorf("dial %s = %v, want ErrPrivate", addr, err)
		}
	}
}

// GuardedTransport must keep the stdlib defaults (proxy, timeouts, pooling)
// while installing the pinning dialer — a bare &http.Transport{} would quietly
// drop proxy support and connection reuse.
func TestGuardedTransport_PreservesDefaultsAndPins(t *testing.T) {
	tr := GuardedTransport(false)
	if tr.DialContext == nil {
		t.Fatal("GuardedTransport did not install a guarded dialer")
	}
	def := http.DefaultTransport.(*http.Transport)
	if tr.MaxIdleConns != def.MaxIdleConns || tr.IdleConnTimeout != def.IdleConnTimeout {
		t.Errorf("defaults not preserved: idle=%d/%v want %d/%v",
			tr.MaxIdleConns, tr.IdleConnTimeout, def.MaxIdleConns, def.IdleConnTimeout)
	}
	if tr.Proxy == nil {
		t.Error("proxy support dropped")
	}
	if _, err := tr.DialContext(context.Background(), "tcp", "127.0.0.1:80"); !errors.Is(err, ErrPrivate) {
		t.Errorf("guarded transport dialed loopback: %v", err)
	}
}
