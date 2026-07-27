package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/brandonhon/ember/internal/urlcheck"
)

// Subscription mirrors the rows in push_subscriptions. The store package
// owns the persistence; we accept the smaller surface so notify.go
// doesn't pull in the store transitively.
type Subscription struct {
	ID       int64
	Endpoint string
	P256dh   string
	Auth     string
}

// SubStore is the minimum store surface the Notifier needs.
type SubStore interface {
	ListSubscriptionsForUser(ctx context.Context, userID int64) ([]Subscription, error)
	DeleteSubscriptionByEndpoint(ctx context.Context, endpoint string) error
}

// Payload is the data sent to the browser. The service worker receives
// it as the .data on the PushEvent.
type Payload struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	URL       string `json:"url,omitempty"`
	ArticleID int64  `json:"article_id,omitempty"`
}

// Notifier fans out a payload to every subscription registered by a
// user. Holds the VAPID keypair + the admin contact "mailto:" subject.
// Construct once at boot; safe for concurrent use.
type Notifier struct {
	keys       Keys
	subject    string // "mailto:..."
	store      SubStore
	logger     *slog.Logger
	httpClient *http.Client
}

// PublicKey returns the VAPID public key for the SPA to use with
// pushManager.subscribe.
func (n *Notifier) PublicKey() string {
	if n == nil {
		return ""
	}
	return n.keys.PublicKey
}

// NewNotifier returns a configured fan-out. subject must be a valid
// "mailto:..." or "https://..." per the VAPID spec; falls back to a
// localhost address (logged warn) if empty. allowPrivate disables the
// SSRF redirect guard (matches the feed fetcher's flag) for homelab setups
// where push services may live on a private network.
func NewNotifier(keys Keys, contactEmail string, store SubStore, logger *slog.Logger, allowPrivate bool) *Notifier {
	if logger == nil {
		logger = slog.Default()
	}
	subject := "mailto:" + contactEmail
	if contactEmail == "" {
		subject = "mailto:admin@localhost"
		logger.Warn("VAPID subject defaulted; set EMBER_ADMIN_EMAIL or the admin user's email for better deliverability")
	}
	return &Notifier{
		keys:    keys,
		subject: subject,
		store:   store,
		logger:  logger.With("component", "push"),
		// SSRF guard: the subscription endpoint is validated at registration,
		// but a push service could 30x to a private/metadata address. Re-check
		// every redirect hop, mirroring the feed fetcher.
		httpClient: &http.Client{
			// IP-pinning transport closes the DNS-rebind window between the
			// registration-time Check and the actual delivery dial.
			Transport: urlcheck.GuardedTransport(allowPrivate),
			CheckRedirect: func(req *http.Request, _ []*http.Request) error {
				return urlcheck.Check(req.Context(), req.URL.String(), allowPrivate)
			},
		},
	}
}

// NotifyUser delivers payload to every subscription registered by userID.
// Dead subscriptions (404 / 410 from the push service) are dropped from
// the DB. Returns (sent, removed) counts. Non-fatal: callers can ignore
// the return values and rely on logged details.
//
// Fan-out runs in parallel — even one slow push service shouldn't gate
// the others. Each send respects ctx; a cancelled ctx will return
// promptly with whatever was sent up to that point.
func (n *Notifier) NotifyUser(ctx context.Context, userID int64, p Payload) (sent, removed int) {
	if n == nil {
		return 0, 0
	}
	subs, err := n.store.ListSubscriptionsForUser(ctx, userID)
	if err != nil {
		n.logger.Error("push: list subscriptions", "user_id", userID, "error", err)
		return 0, 0
	}
	if len(subs) == 0 {
		return 0, 0
	}
	body, err := json.Marshal(p)
	if err != nil {
		n.logger.Error("push: marshal payload", "error", err)
		return 0, 0
	}

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)
	for _, sub := range subs {
		wg.Add(1)
		go func(sub Subscription) {
			defer wg.Done()
			// Each send needs its OWN copy of the payload. webpush-go does
			// `dataBuf := bytes.NewBuffer(message)` and then appends a padding
			// delimiter + padding, which writes into the spare capacity of the
			// slice we hand it — so sharing one json.Marshal result across the
			// fan-out has every goroutine scribbling on the same backing array.
			// Caught by `go test -race` once a test finally drove the fan-out
			// with more than one subscription.
			msg := bytes.Clone(body)
			s := &webpush.Subscription{
				Endpoint: sub.Endpoint,
				Keys: webpush.Keys{
					P256dh: sub.P256dh,
					Auth:   sub.Auth,
				},
			}
			opts := &webpush.Options{
				HTTPClient:      n.httpClient,
				Subscriber:      n.subject,
				VAPIDPublicKey:  n.keys.PublicKey,
				VAPIDPrivateKey: n.keys.PrivateKey,
				TTL:             86400,
				Urgency:         webpush.UrgencyNormal,
			}
			resp, err := webpush.SendNotificationWithContext(ctx, msg, s, opts)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					n.logger.Warn("push: send failed", "endpoint", redactEndpoint(sub.Endpoint), "error", err)
				}
				return
			}
			defer resp.Body.Close()
			switch {
			case resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound:
				// The browser uninstalled the subscription. Drop the
				// row so we stop trying.
				if derr := n.store.DeleteSubscriptionByEndpoint(ctx, sub.Endpoint); derr != nil {
					n.logger.Warn("push: cleanup dead sub failed", "endpoint", redactEndpoint(sub.Endpoint), "error", derr)
				} else {
					mu.Lock()
					removed++
					mu.Unlock()
				}
			case resp.StatusCode >= 200 && resp.StatusCode < 300:
				mu.Lock()
				sent++
				mu.Unlock()
			default:
				n.logger.Warn("push: unexpected status", "endpoint", redactEndpoint(sub.Endpoint), "status", resp.StatusCode)
			}
		}(sub)
	}
	wg.Wait()
	return sent, removed
}

// redactEndpoint trims the endpoint URL down to scheme://host so logs don't
// carry the full per-device token (sensitive if logs leaked). Mozilla /
// Google / Apple endpoints all carry the token in the path. Deliberately
// string-based rather than url.Parse — this is a log field, and a malformed
// endpoint should degrade to "log it as-is", not error.
func redactEndpoint(endpoint string) string {
	if endpoint == "" {
		return ""
	}
	i := strings.Index(endpoint, "://")
	if i < 0 {
		return endpoint
	}
	afterScheme := i + len("://")
	// j > 0 keeps the original behavior for the degenerate "scheme:///path"
	// case: with an empty host there is nothing to redact down to, so the
	// endpoint is returned unchanged.
	if j := strings.IndexByte(endpoint[afterScheme:], '/'); j > 0 {
		return endpoint[:afterScheme+j]
	}
	return endpoint
}
