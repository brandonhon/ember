package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/brandonhon/ember/internal/store"
)

// secretKey is deliberately distinctive so a substring search over a whole
// response body is a meaningful leak check.
const secretKey = "sk-ember-test-not-a-real-key-9f3a"

// newBackendHarness starts on the Ollama backend, the way a fresh install does.
func newBackendHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessWith(t, func(d *Dependencies) {
		wireOllama(d, "http://ollama.invalid", "llama3")
		d.BackendFallbacks = BackendFallbacks{
			Backend:       store.BackendOllama,
			OllamaBaseURL: "http://ollama.invalid",
			OllamaModel:   "llama3",
		}
	})
}

// postRaw posts body and returns the status plus the undecoded response, so a
// test can assert on what the wire actually carried rather than on the fields
// it happened to decode.
func postRaw(t *testing.T, c *http.Client, url string, body any) (int, string) {
	t.Helper()
	var raw json.RawMessage
	code := post(t, c, url, body, &raw)
	return code, string(raw)
}

func getLLM(t *testing.T, c *http.Client, base string) (llmStatus, string) {
	t.Helper()
	var raw json.RawMessage
	if code := get(t, c, base+"/api/admin/llm", &raw); code != http.StatusOK {
		t.Fatalf("GET /api/admin/llm = %d", code)
	}
	var env struct {
		Data llmStatus `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode llm status: %v (%s)", err, raw)
	}
	return env.Data, string(raw)
}

// The whole point of the feature: an admin picks a backend, it is persisted,
// it comes back on the next read, and the live Switcher is swapped so the next
// article goes to the new provider without a restart.
//
// The API key is the part that must never come back. It is a paid credential,
// and echoing it would put it in every admin's browser session and in any log
// or proxy trace of this response — so the assertions search the raw body, not
// just the field the client struct happens to expose.
func TestLLMBackend_RoundTripNeverEchoesTheKey(t *testing.T) {
	h := newBackendHarness(t)
	h.seedUser(t, "root", "p", true)
	c := h.login(t, "root", "p")
	ctx := context.Background()

	code, raw := postRaw(t, c, h.srv.URL+"/api/admin/llm/backend", map[string]any{
		"backend": "anthropic", "api_key": secretKey, "model": "claude-opus-5",
	})
	if code != http.StatusOK {
		t.Fatalf("POST backend = %d: %s", code, raw)
	}
	if strings.Contains(raw, secretKey) {
		t.Fatalf("the save response echoed the API key: %s", raw)
	}
	if !strings.Contains(raw, `"api_key_set":true`) {
		t.Errorf("save response should report api_key_set=true, got %s", raw)
	}

	status, rawGet := getLLM(t, c, h.srv.URL)
	if strings.Contains(rawGet, secretKey) {
		t.Fatalf("GET /api/admin/llm echoed the API key: %s", rawGet)
	}
	if status.Backend != store.BackendAnthropic || status.Model != "claude-opus-5" {
		t.Errorf("backend/model = %q/%q, want anthropic/claude-opus-5", status.Backend, status.Model)
	}
	if !status.APIKeySet {
		t.Error("api_key_set = false after storing a key")
	}
	if !status.Enabled {
		t.Error("enabled = false with a fully configured Claude backend")
	}

	// The live Switcher was swapped, and the Ollama client went away with it —
	// model management is Ollama-only, so pull must now stand down rather than
	// talk to a daemon that is no longer the backend.
	if h.dep.Backend.Get() == nil {
		t.Error("the Switcher still has no backend after a successful save")
	}
	if code, raw := postRaw(t, c, h.srv.URL+"/api/admin/llm/pull",
		map[string]any{"model": "llama3"}); code != http.StatusServiceUnavailable {
		t.Errorf("pull on the Claude backend = %d, want 503: %s", code, raw)
	} else if !strings.Contains(raw, "not_ollama") {
		t.Errorf("pull error code = %s, want not_ollama", raw)
	}

	// An empty api_key means "no change" — the SPA never received the key, so
	// it cannot send it back, and treating "" as an erase would silently
	// unconfigure the backend on every unrelated save.
	if code, raw := postRaw(t, c, h.srv.URL+"/api/admin/llm/backend", map[string]any{
		"backend": "anthropic", "model": "claude-haiku-4-5",
	}); code != http.StatusOK {
		t.Fatalf("re-save without a key = %d: %s", code, raw)
	}
	live := h.store.ResolveBackendSettings(ctx, store.BackendSettings{})
	if live.APIKey != secretKey {
		t.Errorf("stored key = %q after a keyless save, want it untouched", live.APIKey)
	}
	if live.Model != "claude-haiku-4-5" {
		t.Errorf("stored model = %q, want claude-haiku-4-5", live.Model)
	}

	// clear_api_key is the only way to erase one. With the credential gone the
	// backend is no longer usable, so the Switcher unwires and the summary gate
	// closes rather than queueing articles at an endpoint that will 401.
	if code, raw := postRaw(t, c, h.srv.URL+"/api/admin/llm/backend", map[string]any{
		"backend": "ollama", "clear_api_key": true,
	}); code != http.StatusOK {
		t.Fatalf("clear = %d: %s", code, raw)
	}
	if live := h.store.ResolveBackendSettings(ctx, store.BackendSettings{}); live.APIKey != "" {
		t.Errorf("stored key = %q after clear_api_key", live.APIKey)
	}
	// Back on Ollama, the base URL and model fall back to the env defaults
	// rather than inheriting Claude's, and model management answers again.
	status, _ = getLLM(t, c, h.srv.URL)
	if status.Backend != store.BackendOllama || status.BaseURL != "http://ollama.invalid" {
		t.Errorf("after switching back: backend=%q base_url=%q, want ollama/http://ollama.invalid",
			status.Backend, status.BaseURL)
	}
	if status.APIKeySet {
		t.Error("api_key_set = true after clear_api_key")
	}
}

// Every rejection here describes a backend that cannot answer. Storing one
// would queue every incoming article at a dead endpoint, so the request must
// fail AND leave both the persisted settings and the live Switcher untouched —
// a 400 that still swapped the backend is the damaging outcome.
func TestLLMBackend_RejectsUnusableConfigurations(t *testing.T) {
	h := newBackendHarness(t)
	h.seedUser(t, "root", "p", true)
	c := h.login(t, "root", "p")
	ctx := context.Background()
	before := h.dep.Backend.Get()

	cases := []struct {
		name string
		body map[string]any
	}{
		{"unknown backend", map[string]any{"backend": "gpt5-please", "base_url": "https://x.test"}},
		{"empty backend", map[string]any{"backend": ""}},
		{"openai without a base url", map[string]any{"backend": "openai", "model": "gpt-4o-mini"}},
		{"openai without a model", map[string]any{
			"backend": "openai", "base_url": "https://api.groq.com/openai/v1"}},
		{"anthropic without a key", map[string]any{"backend": "anthropic", "model": "claude-opus-5"}},
		{"non-http scheme", map[string]any{
			"backend": "openai", "base_url": "file:///etc/passwd", "model": "m"}},
		{"scheme-less base url", map[string]any{
			"backend": "openai", "base_url": "api.openai.com/v1", "model": "m"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, raw := postRaw(t, c, h.srv.URL+"/api/admin/llm/backend", tc.body)
			if code != http.StatusBadRequest {
				t.Fatalf("POST %v = %d, want 400: %s", tc.body, code, raw)
			}
			live := h.store.ResolveBackendSettings(ctx, store.BackendSettings{Backend: store.BackendOllama})
			if live.Backend != store.BackendOllama || live.BaseURL != "" || live.APIKey != "" {
				t.Errorf("a rejected request persisted %+v", live)
			}
			if h.dep.Backend.Get() != before {
				t.Error("a rejected request swapped the live backend")
			}
		})
	}
}

// The backend decides where every user's article text is sent and holds a paid
// credential, so it is admin-only. A 403 that still changed something would be
// the whole point of the check defeated.
func TestLLMBackend_NonAdminForbiddenAndChangesNothing(t *testing.T) {
	h := newBackendHarness(t)
	h.seedUser(t, "reader", "p", false)
	c := h.login(t, "reader", "p")
	ctx := context.Background()
	before := h.dep.Backend.Get()

	code, raw := postRaw(t, c, h.srv.URL+"/api/admin/llm/backend", map[string]any{
		"backend": "anthropic", "api_key": secretKey, "model": "claude-opus-5",
	})
	if code != http.StatusForbidden {
		t.Fatalf("non-admin POST backend = %d, want 403: %s", code, raw)
	}
	live := h.store.ResolveBackendSettings(ctx, store.BackendSettings{Backend: store.BackendOllama})
	if live.Backend != store.BackendOllama || live.APIKey != "" {
		t.Errorf("non-admin request persisted %+v", live)
	}
	if h.dep.Backend.Get() != before {
		t.Error("non-admin request swapped the live backend")
	}
	if code := get(t, c, h.srv.URL+"/api/admin/llm", nil); code != http.StatusForbidden {
		t.Errorf("non-admin GET /api/admin/llm = %d, want 403", code)
	}
}

// d.Ollama is reassigned by this handler while the neighbouring admin handlers
// read it. Under -race a bare field assignment fails here; the atomic holder
// installed by NewRouter is what makes it safe. The Ollama client itself has a
// companion test (internal/summarize/client_race_test.go) because this exact
// class of bug already bit that struct.
func TestLLMBackend_SwapRacesWithReads(t *testing.T) {
	h := newBackendHarness(t)
	h.seedUser(t, "root", "p", true)
	c := h.login(t, "root", "p")

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 12 {
			body := map[string]any{"backend": "anthropic", "api_key": secretKey, "model": "claude-opus-5"}
			if i%2 == 0 {
				body = map[string]any{"backend": "ollama"}
			}
			if code, raw := postRaw(t, c, h.srv.URL+"/api/admin/llm/backend", body); code != http.StatusOK {
				t.Errorf("swap %d = %d: %s", i, code, raw)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 12 {
			// Reads d.Ollama through the holder; on the Ollama backend it also
			// calls Model()/Options() on the client being swapped out.
			if code := get(t, c, h.srv.URL+"/api/admin/llm", nil); code != http.StatusOK {
				t.Errorf("status read = %d", code)
				return
			}
		}
	}()
	wg.Wait()
}

// Switching back to Ollama must not inherit the hosted provider's endpoint or
// model. This is the one failure mode the whole two-pair fallback design exists
// to prevent, and it is worth a test of its own because it defeats the
// readiness gate rather than tripping it: NewOllama("https://api.groq.com/...",
// "llama-3.3-70b") is a perfectly non-nil client, so Configured() is true, the
// gate never fires, and every article fails and is stamped 'skipped' — terminal,
// and undone only by a full Resummarize.
//
// The request deliberately carries the hosted values, which is exactly what a
// curl caller or a stale form would send. The server, not the SPA, is what has
// to drop them.
func TestLLMBackend_SwitchingBackToOllamaDropsTheHostedEndpoint(t *testing.T) {
	h := newBackendHarness(t)
	h.seedUser(t, "root", "p", true)
	c := h.login(t, "root", "p")
	ctx := context.Background()

	const hostedURL = "https://api.groq.com/openai/v1"
	const hostedModel = "llama-3.3-70b"

	// A model chosen in the picker, which the Ollama backend must keep using.
	if err := h.store.PutAppSetting(ctx, keyOllamaModel, "qwen2.5:3b"); err != nil {
		t.Fatal(err)
	}

	if code, raw := postRaw(t, c, h.srv.URL+"/api/admin/llm/backend", map[string]any{
		"backend": "openai", "base_url": hostedURL, "model": hostedModel,
	}); code != http.StatusOK {
		t.Fatalf("switch to openai = %d: %s", code, raw)
	}

	if code, raw := postRaw(t, c, h.srv.URL+"/api/admin/llm/backend", map[string]any{
		"backend": "ollama", "base_url": hostedURL, "model": hostedModel,
	}); code != http.StatusOK {
		t.Fatalf("switch back to ollama = %d: %s", code, raw)
	}

	live := ResolveBackend(ctx, h.store, h.dep.BackendFallbacks)
	if live.Backend != store.BackendOllama {
		t.Fatalf("backend = %q, want ollama", live.Backend)
	}
	if live.BaseURL != "http://ollama.invalid" {
		t.Errorf("base_url = %q, want the Ollama endpoint — the hosted URL bled across the switch", live.BaseURL)
	}
	if live.Model != "qwen2.5:3b" {
		t.Errorf("model = %q, want the ollama_model row the picker wrote", live.Model)
	}

	// The constructed client is what actually talks to a daemon, so assert on
	// it and not only on the settings that fed it.
	sum, oll := BuildBackend(live)
	if sum == nil || oll == nil {
		t.Fatalf("BuildBackend returned (%v, %v), want a usable Ollama client", sum, oll)
	}
	if oll.BaseURL != "http://ollama.invalid" || oll.Model() != "qwen2.5:3b" {
		t.Errorf("built Ollama client at %q model %q", oll.BaseURL, oll.Model())
	}

	// And the admin sees the same thing the server will use.
	status, _ := getLLM(t, c, h.srv.URL)
	if status.BaseURL != "http://ollama.invalid" || status.Model != "qwen2.5:3b" {
		t.Errorf("GET reports base_url=%q model=%q", status.BaseURL, status.Model)
	}
}
