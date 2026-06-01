package push

import (
	"context"
	"errors"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// freshKeys generates a real VAPID keypair via the library. Used by every
// test below — the library refuses to construct Notifier-bound options
// with invalid keys.
func freshKeys(t *testing.T) Keys {
	t.Helper()
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("generate vapid: %v", err)
	}
	return Keys{PublicKey: pub, PrivateKey: priv}
}

// stubStore is a minimal SubStore for the tests below — returns either
// a fixed list or an error, records deletions.
type stubStore struct {
	subs    []Subscription
	listErr error
	deleted []string
}

func (s *stubStore) ListSubscriptionsForUser(context.Context, int64) ([]Subscription, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.subs, nil
}

func (s *stubStore) DeleteSubscriptionByEndpoint(_ context.Context, endpoint string) error {
	s.deleted = append(s.deleted, endpoint)
	return nil
}

func TestNotifyUser_NilNotifier(t *testing.T) {
	var n *Notifier
	sent, removed := n.NotifyUser(context.Background(), 1, Payload{Title: "x"})
	if sent != 0 || removed != 0 {
		t.Errorf("nil notifier must short-circuit; got sent=%d removed=%d", sent, removed)
	}
	if pk := n.PublicKey(); pk != "" {
		t.Errorf("nil notifier must return empty public key; got %q", pk)
	}
}

func TestNotifyUser_NoSubscriptions(t *testing.T) {
	n := NewNotifier(freshKeys(t), "test@example.com", &stubStore{}, nil)
	sent, removed := n.NotifyUser(context.Background(), 1, Payload{Title: "x"})
	if sent != 0 || removed != 0 {
		t.Errorf("no subs must yield zeros; got sent=%d removed=%d", sent, removed)
	}
}

func TestNotifyUser_ListError(t *testing.T) {
	n := NewNotifier(freshKeys(t), "test@example.com", &stubStore{listErr: errors.New("boom")}, nil)
	sent, removed := n.NotifyUser(context.Background(), 1, Payload{Title: "x"})
	if sent != 0 || removed != 0 {
		t.Errorf("list error must yield zeros; got sent=%d removed=%d", sent, removed)
	}
}

func TestNewNotifier_DefaultSubject(t *testing.T) {
	// Empty contact email — should still construct (the warn-and-default
	// path) and yield a Notifier whose PublicKey() returns the keypair.
	keys := freshKeys(t)
	n := NewNotifier(keys, "", &stubStore{}, nil)
	if n == nil {
		t.Fatal("expected notifier")
	}
	if n.PublicKey() != keys.PublicKey {
		t.Errorf("PublicKey mismatch; got %q want %q", n.PublicKey(), keys.PublicKey)
	}
}

func TestLoadOrCreateKeys_GeneratesAndPersists(t *testing.T) {
	store := &memKeyStore{kv: map[string]string{}}
	ctx := context.Background()

	keys1, err := LoadOrCreateKeys(ctx, store)
	if err != nil {
		t.Fatalf("LoadOrCreateKeys initial: %v", err)
	}
	if keys1.PublicKey == "" || keys1.PrivateKey == "" {
		t.Fatal("expected non-empty generated keys")
	}
	// Second call must return the same persisted pair — auto-rotation
	// would invalidate every existing browser subscription.
	keys2, err := LoadOrCreateKeys(ctx, store)
	if err != nil {
		t.Fatalf("LoadOrCreateKeys reload: %v", err)
	}
	if keys2 != keys1 {
		t.Errorf("expected persistent keypair across calls; got %+v then %+v", keys1, keys2)
	}
}

type memKeyStore struct {
	kv map[string]string
}

func (m *memKeyStore) GetAppSetting(_ context.Context, key string) (string, error) {
	return m.kv[key], nil
}

func (m *memKeyStore) PutAppSetting(_ context.Context, key, value string) error {
	m.kv[key] = value
	return nil
}

func TestRedactEndpoint(t *testing.T) {
	cases := map[string]string{
		"": "",
		"https://fcm.googleapis.com/fcm/send/AAAAAA": "https://fcm.googleapis.com",
		"https://updates.push.services.mozilla.com/wpush/v2/xxxxx": "https://updates.push.services.mozilla.com",
		"https://example.com": "https://example.com",
	}
	for in, want := range cases {
		if got := redactEndpoint(in); got != want {
			t.Errorf("redactEndpoint(%q) = %q, want %q", in, got, want)
		}
	}
}
