package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/push"
)

// withPush wires a real (non-nil) push.Notifier so the /me/push-subscriptions
// handlers pass their feature gate. The notifier never sends during these
// tests — registration only stores a row.
func withPush(allowPrivate bool) func(*Dependencies) {
	return func(d *Dependencies) {
		d.AllowPrivateURLs = allowPrivate
		keys, err := push.LoadOrCreateKeys(context.Background(), d.Store)
		if err != nil {
			panic(err)
		}
		d.Push = push.NewNotifier(keys, "mailto:test@ember.test", d.Store, nil, allowPrivate)
	}
}

func TestValidModelName(t *testing.T) {
	ok := []string{"llama3", "llama3:8b", "library/llama3:latest", "registry.io/ns/m:tag", "qwen2.5-coder"}
	bad := []string{"", "../etc/passwd", "model name", "evil;rm -rf", "a\nb", "registry.io/<script>"}
	for _, m := range ok {
		if !validModelName.MatchString(m) {
			t.Errorf("validModelName rejected valid %q", m)
		}
	}
	for _, m := range bad {
		if validModelName.MatchString(m) {
			t.Errorf("validModelName accepted invalid %q", m)
		}
	}
}

func TestPushSubscription_SSRFRejected(t *testing.T) {
	// AllowPrivateURLs=false so the SSRF guard is live. Literal IPs avoid DNS.
	h := newHarnessWith(t, withPush(false))
	h.seedUser(t, "alice", "password1", false)
	c := h.login(t, "alice", "password1")

	// Private/metadata target → rejected at registration.
	code := post(t, c, h.srv.URL+"/api/me/push-subscriptions", map[string]string{
		"endpoint": "http://169.254.169.254/push", "p256dh": "x", "auth": "y",
	}, nil)
	if code != http.StatusBadRequest {
		t.Errorf("private endpoint: want 400, got %d", code)
	}

	// Public literal IP → accepted (stored; no send happens).
	code = post(t, c, h.srv.URL+"/api/me/push-subscriptions", map[string]string{
		"endpoint": "https://93.184.216.34/push", "p256dh": "x", "auth": "y",
	}, nil)
	if code != http.StatusOK {
		t.Errorf("public endpoint: want 200, got %d", code)
	}
}

func TestPushSubscription_CrossUserConflict(t *testing.T) {
	h := newHarnessWith(t, withPush(true)) // allowPrivate: skip SSRF for synthetic endpoint
	h.seedUser(t, "alice", "password1", false)
	h.seedUser(t, "bob", "password2", false)
	const ep = "https://push.example/ep/shared"

	ac := h.login(t, "alice", "password1")
	if code := post(t, ac, h.srv.URL+"/api/me/push-subscriptions", map[string]string{
		"endpoint": ep, "p256dh": "p", "auth": "a",
	}, nil); code != http.StatusOK {
		t.Fatalf("alice register: want 200, got %d", code)
	}

	// Bob submits Alice's endpoint → must be rejected (409), not hijacked.
	bc := h.login(t, "bob", "password2")
	if code := post(t, bc, h.srv.URL+"/api/me/push-subscriptions", map[string]string{
		"endpoint": ep, "p256dh": "pX", "auth": "aX",
	}, nil); code != http.StatusConflict {
		t.Errorf("bob hijack: want 409, got %d", code)
	}
}

func TestCreateFilter_PerUserCap(t *testing.T) {
	h := newHarness(t)
	u := h.seedUser(t, "alice", "password1", false)
	ctx := context.Background()
	// Seed the cap directly via the store (fast; bypasses the handler).
	for i := 0; i < maxFiltersPerUser; i++ {
		if _, err := h.store.CreateFilter(ctx, models.Filter{
			UserID: u.ID, Name: "f", Action: "mark_read", Enabled: true, Priority: 100,
			MatchJSON: `{"field":"title","op":"contains","value":"x"}`,
		}); err != nil {
			t.Fatalf("seed filter %d: %v", i, err)
		}
	}
	c := h.login(t, "alice", "password1")
	var body struct {
		Error struct{ Code string } `json:"error"`
	}
	code := post(t, c, h.srv.URL+"/api/filters", map[string]any{
		"name": "one too many", "action": "mark_read",
		"match_json": `{"field":"title","op":"contains","value":"y"}`,
	}, &body)
	if code != http.StatusBadRequest || body.Error.Code != "filter_limit" {
		t.Errorf("201st filter: want 400/filter_limit, got %d/%q", code, body.Error.Code)
	}
}
