package emailinbox

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeIngester resolves a fixed set of handles and records every ingest.
type fakeIngester struct {
	mu       sync.Mutex
	handles  map[string][2]int64 // handle -> {userID, feedID}
	resolveE error
	ingested []ingest
	ingestE  error
}

type ingest struct {
	userID, feedID int64
	body           string
}

func (f *fakeIngester) ResolveInbox(_ context.Context, handle string) (int64, int64, bool, error) {
	if f.resolveE != nil {
		return 0, 0, false, f.resolveE
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	ids, ok := f.handles[handle]
	if !ok {
		return 0, 0, false, nil
	}
	return ids[0], ids[1], true, nil
}

func (f *fakeIngester) IngestEmail(_ context.Context, userID, feedID int64, raw []byte) error {
	if f.ingestE != nil {
		return f.ingestE
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ingested = append(f.ingested, ingest{userID, feedID, string(raw)})
	return nil
}

func (f *fakeIngester) all() []ingest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ingest(nil), f.ingested...)
}

const (
	handleA = "ABCDEFGHJKMN"
	handleB = "PQRSTVWXYZ01"
)

// startServer runs the SMTP listener on an ephemeral port and returns its
// address plus the fake store.
func startServer(t *testing.T, mutate func(*Config)) (string, *fakeIngester) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	cfg := Config{Domain: "mail.test", ListenAddr: addr, MaxBytes: 4096, ReadTimeout: 5 * time.Second}
	if mutate != nil {
		mutate(&cfg)
	}
	store := &fakeIngester{handles: map[string][2]int64{
		handleA: {7, 70},
		handleB: {8, 80},
	}}
	srv := NewServer(cfg, store, nil)
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}
	go func() { _ = srv.Start() }()
	t.Cleanup(srv.Stop)

	// Wait for the listener to accept.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return addr, store
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("smtp listener never came up")
	return "", nil
}

func send(t *testing.T, addr, from string, rcpts []string, body string) error {
	t.Helper()
	c, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, r := range rcpts {
		if err := c.Rcpt(r); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(body)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func TestSMTP_DeliversToAKnownHandle(t *testing.T) {
	addr, store := startServer(t, nil)
	body := "Subject: hi\r\n\r\nbody\r\n"
	if err := send(t, addr, "sender@example.com", []string{handleA + "@mail.test"}, body); err != nil {
		t.Fatalf("send: %v", err)
	}
	got := store.all()
	if len(got) != 1 {
		t.Fatalf("ingested %d messages, want 1", len(got))
	}
	if got[0].userID != 7 || got[0].feedID != 70 {
		t.Errorf("delivered to %d/%d, want 7/70", got[0].userID, got[0].feedID)
	}
	if !strings.Contains(got[0].body, "body") {
		t.Errorf("body not carried through: %q", got[0].body)
	}
}

// MaxRecipients is 5, so the server ACCEPTS several recipients in one
// transaction. Every accepted recipient must actually receive the message —
// silently dropping all but the last is mail loss.
func TestSMTP_DeliversToEveryAcceptedRecipient(t *testing.T) {
	addr, store := startServer(t, nil)
	body := "Subject: newsletter\r\n\r\nhello both\r\n"
	if err := send(t, addr, "list@example.com",
		[]string{handleA + "@mail.test", handleB + "@mail.test"}, body); err != nil {
		t.Fatalf("send: %v", err)
	}
	got := store.all()
	if len(got) != 2 {
		t.Fatalf("ingested %d messages for 2 accepted recipients, want 2 — "+
			"mail addressed to several inboxes on this domain is being dropped", len(got))
	}
	seen := map[int64]bool{}
	for _, g := range got {
		seen[g.userID] = true
	}
	if !seen[7] || !seen[8] {
		t.Errorf("delivered to %v, want both user 7 and user 8", seen)
	}
}

// The listener must not be an open relay: anything outside the configured
// domain, or a well-formed handle with no inbox, is refused at RCPT.
func TestSMTP_RefusesForeignAndUnknownRecipients(t *testing.T) {
	addr, store := startServer(t, nil)
	for _, rcpt := range []string{
		"victim@elsewhere.com",       // different domain — relay attempt
		"ZZZZZZZZZZZZ@mail.test",     // valid shape, no such inbox
		"short@mail.test",            // invalid handle shape
		"ABCDEFGHJKMN@evil.test",     // right handle, wrong domain
		"ABCDEFGHJKMN@sub.mail.test", // subdomain must not match
	} {
		err := send(t, addr, "sender@example.com", []string{rcpt}, "Subject: x\r\n\r\nx\r\n")
		if err == nil {
			t.Errorf("accepted mail for %s — possible open relay", rcpt)
		}
	}
	if n := len(store.all()); n != 0 {
		t.Errorf("%d messages ingested for refused recipients", n)
	}
}

// A message larger than MaxBytes is rejected rather than ingested.
func TestSMTP_RejectsOversizeMessage(t *testing.T) {
	addr, store := startServer(t, func(c *Config) { c.MaxBytes = 512 })
	big := "Subject: big\r\n\r\n" + strings.Repeat("A", 4096) + "\r\n"
	if err := send(t, addr, "s@example.com", []string{handleA + "@mail.test"}, big); err == nil {
		t.Error("oversize message accepted")
	}
	if n := len(store.all()); n != 0 {
		t.Errorf("%d oversize messages ingested", n)
	}
}

// A transient store failure must be a 4xx (retryable) so the sender queues
// and retries, not a 5xx that would discard the newsletter permanently.
func TestSMTP_StoreFailuresAreRetryable(t *testing.T) {
	addr, store := startServer(t, nil)
	store.ingestE = fmt.Errorf("db down")
	err := send(t, addr, "s@example.com", []string{handleA + "@mail.test"}, "Subject: x\r\n\r\nx\r\n")
	if err == nil {
		t.Fatal("ingest failure was reported as success")
	}
	if !strings.Contains(err.Error(), "451") {
		t.Errorf("ingest failure = %v, want a 4xx retryable code", err)
	}

	store.ingestE = nil
	store.resolveE = fmt.Errorf("db down")
	err = send(t, addr, "s@example.com", []string{handleA + "@mail.test"}, "Subject: x\r\n\r\nx\r\n")
	if err == nil {
		t.Fatal("resolve failure was reported as success")
	}
	if !strings.Contains(err.Error(), "451") {
		t.Errorf("resolve failure = %v, want a 4xx retryable code", err)
	}
}

func TestNewServer_DisabledWithoutDomain(t *testing.T) {
	if s := NewServer(Config{}, &fakeIngester{}, nil); s != nil {
		t.Error("NewServer should return nil when Domain is empty (feature disabled)")
	}
}
