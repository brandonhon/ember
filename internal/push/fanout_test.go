package push

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// browserKeys produces a subscription keypair in the shape a real browser
// sends: P256dh is the uncompressed P-256 public point, Auth is 16 random
// bytes, both base64url-unpadded. webpush-go refuses to encrypt without
// well-formed values, so the fan-out can't be exercised with dummy strings.
func browserKeys(t *testing.T) (p256dh, auth string) {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var a [16]byte
	if _, err := rand.Read(a[:]); err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString(k.PublicKey().Bytes()), enc.EncodeToString(a[:])
}

// pushServer stands in for a browser push service, returning a scripted
// status per request and recording how many it received.
func pushServer(t *testing.T, status func(n int) int) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(hits.Add(1))
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(status(n))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// notifierFor builds a Notifier wired to the given store. allowPrivate must be
// true: httptest listens on loopback, which the SSRF guard blocks otherwise.
func notifierFor(t *testing.T, st SubStore) *Notifier {
	t.Helper()
	return NewNotifier(freshKeys(t), "admin@example.test", st,
		slog.New(slog.NewTextHandler(io.Discard, nil)), true)
}

func subFor(t *testing.T, id int64, endpoint string) Subscription {
	t.Helper()
	p, a := browserKeys(t)
	return Subscription{ID: id, Endpoint: endpoint, P256dh: p, Auth: a}
}

// A 2xx from the push service counts as sent and leaves the row alone.
func TestNotifyUser_DeliversToEverySubscription(t *testing.T) {
	srv, hits := pushServer(t, func(int) int { return http.StatusCreated })
	st := &stubStore{subs: []Subscription{
		subFor(t, 1, srv.URL+"/dev1"),
		subFor(t, 2, srv.URL+"/dev2"),
		subFor(t, 3, srv.URL+"/dev3"),
	}}
	sent, removed := notifierFor(t, st).NotifyUser(context.Background(), 7, Payload{Title: "hi", Body: "there"})
	if sent != 3 || removed != 0 {
		t.Errorf("sent=%d removed=%d, want 3/0", sent, removed)
	}
	if got := hits.Load(); got != 3 {
		t.Errorf("push service saw %d requests, want 3", got)
	}
	if len(st.deleted) != 0 {
		t.Errorf("healthy subscriptions were deleted: %v", st.deleted)
	}
}

// 410 Gone / 404 mean the browser dropped the subscription — the row must be
// removed so we stop pushing to it, and it must not count as sent.
func TestNotifyUser_RemovesDeadSubscriptions(t *testing.T) {
	for _, status := range []int{http.StatusGone, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv, _ := pushServer(t, func(int) int { return status })
			dead := srv.URL + "/dead"
			st := &stubStore{subs: []Subscription{subFor(t, 1, dead)}}
			sent, removed := notifierFor(t, st).NotifyUser(context.Background(), 7, Payload{Title: "x"})
			if sent != 0 || removed != 1 {
				t.Errorf("sent=%d removed=%d, want 0/1", sent, removed)
			}
			if len(st.deleted) != 1 || st.deleted[0] != dead {
				t.Errorf("deleted = %v, want [%s]", st.deleted, dead)
			}
		})
	}
}

// An unexpected status is logged and skipped: not counted, not deleted. A
// transient 500 from a push service must not cost the user their device.
func TestNotifyUser_UnexpectedStatusKeepsSubscription(t *testing.T) {
	srv, _ := pushServer(t, func(int) int { return http.StatusInternalServerError })
	st := &stubStore{subs: []Subscription{subFor(t, 1, srv.URL+"/dev")}}
	sent, removed := notifierFor(t, st).NotifyUser(context.Background(), 7, Payload{Title: "x"})
	if sent != 0 || removed != 0 {
		t.Errorf("sent=%d removed=%d, want 0/0", sent, removed)
	}
	if len(st.deleted) != 0 {
		t.Errorf("a 500 removed the subscription: %v", st.deleted)
	}
}

// Mixed outcomes across devices are tallied independently — one dead device
// must not suppress delivery to the others.
func TestNotifyUser_MixedOutcomes(t *testing.T) {
	srv, _ := pushServer(t, func(n int) int {
		switch n {
		case 1:
			return http.StatusCreated
		case 2:
			return http.StatusGone
		default:
			return http.StatusInternalServerError
		}
	})
	st := &stubStore{subs: []Subscription{
		subFor(t, 1, srv.URL+"/a"),
		subFor(t, 2, srv.URL+"/b"),
		subFor(t, 3, srv.URL+"/c"),
	}}
	sent, removed := notifierFor(t, st).NotifyUser(context.Background(), 7, Payload{Title: "x"})
	if sent+removed != 2 {
		t.Errorf("sent=%d removed=%d, want one of each plus one skipped", sent, removed)
	}
}

// A cancelled context returns promptly without counting anything.
func TestNotifyUser_CancelledContext(t *testing.T) {
	srv, _ := pushServer(t, func(int) int { return http.StatusCreated })
	st := &stubStore{subs: []Subscription{subFor(t, 1, srv.URL+"/dev")}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sent, removed := notifierFor(t, st).NotifyUser(ctx, 7, Payload{Title: "x"})
	if sent != 0 || removed != 0 {
		t.Errorf("sent=%d removed=%d on a cancelled context, want 0/0", sent, removed)
	}
}

// The SSRF guard must survive into delivery: with allowPrivate=false a
// loopback endpoint is refused, so nothing is sent and the row is kept (a
// blocked endpoint is not evidence the browser unsubscribed).
func TestNotifyUser_SSRFGuardBlocksPrivateEndpoint(t *testing.T) {
	srv, hits := pushServer(t, func(int) int { return http.StatusCreated })
	st := &stubStore{subs: []Subscription{subFor(t, 1, srv.URL+"/dev")}}
	n := NewNotifier(freshKeys(t), "admin@example.test", st,
		slog.New(slog.NewTextHandler(io.Discard, nil)), false) // allowPrivate = false
	sent, removed := n.NotifyUser(context.Background(), 7, Payload{Title: "x"})
	if sent != 0 || removed != 0 {
		t.Errorf("sent=%d removed=%d, want 0/0 — loopback must be blocked", sent, removed)
	}
	if got := hits.Load(); got != 0 {
		t.Errorf("blocked endpoint still received %d requests", got)
	}
	if len(st.deleted) != 0 {
		t.Errorf("an SSRF-blocked endpoint was deleted: %v", st.deleted)
	}
}

// Endpoint tokens are per-device secrets; logs must carry only the host.
func TestRedactEndpoint_DropsTheToken(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"https://fcm.googleapis.com/fcm/send/SECRET-TOKEN", "https://fcm.googleapis.com"},
		{"https://updates.push.services.mozilla.com/wpush/v2/gAAA", "https://updates.push.services.mozilla.com"},
		{"https://host.example", "https://host.example"},
		{"not-a-url", "not-a-url"},
		{"https://host:8443/path/x", "https://host:8443"},
	} {
		if got := redactEndpoint(tc.in); got != tc.want {
			t.Errorf("redactEndpoint(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if tc.in != "" && strings.Contains(redactEndpoint(tc.in), "SECRET") {
			t.Errorf("token leaked through redaction: %q", redactEndpoint(tc.in))
		}
	}
}
