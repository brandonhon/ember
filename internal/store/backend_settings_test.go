package store

import (
	"context"
	"testing"
)

func TestValidBackend(t *testing.T) {
	for _, ok := range []string{BackendOllama, BackendOpenAI, BackendAnthropic} {
		if !ValidBackend(ok) {
			t.Errorf("ValidBackend(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "Ollama", "gpt", "openai ", "anthropic\n"} {
		if ValidBackend(bad) {
			t.Errorf("ValidBackend(%q) = true — an unrecognized name must not reach BuildBackend", bad)
		}
	}
}

// The env fallback applies until an admin sets a value, then the stored value
// wins — the same overlay ResolveSMTPSettings does, for the same reason: an
// operator sets defaults in .env, an admin adjusts them in the UI.
func TestBackendSettings_ResolveOverlaysFallback(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	fb := BackendSettings{
		Backend: BackendOllama, BaseURL: "http://ollama:11434",
		APIKey: "env-key", Model: "qwen2.5:0.5b",
	}

	if got := s.ResolveBackendSettings(ctx, fb); got != fb {
		t.Errorf("unset: got %+v, want the fallback %+v", got, fb)
	}

	backend, url, model := BackendOpenAI, "https://api.groq.com/openai/v1", "llama-3.3-70b"
	if err := s.PutBackendSettings(ctx, BackendUpdate{
		Backend: &backend, BaseURL: &url, Model: &model,
	}); err != nil {
		t.Fatal(err)
	}
	got := s.ResolveBackendSettings(ctx, fb)
	if got.Backend != BackendOpenAI || got.BaseURL != url || got.Model != model {
		t.Errorf("after put: %+v", got)
	}
	// No key was supplied, so the env one is still in force.
	if got.APIKey != "env-key" {
		t.Errorf("APIKey = %q, want the env fallback to survive an update that didn't mention it", got.APIKey)
	}

	// An empty string clears the override and falls back to the env value —
	// that is what switching back to Ollama relies on to stop inheriting the
	// hosted provider's URL.
	empty := ""
	if err := s.PutBackendSettings(ctx, BackendUpdate{BaseURL: &empty, Model: &empty}); err != nil {
		t.Fatal(err)
	}
	got = s.ResolveBackendSettings(ctx, fb)
	if got.BaseURL != fb.BaseURL || got.Model != fb.Model {
		t.Errorf("after clearing: base_url=%q model=%q, want the fallbacks back", got.BaseURL, got.Model)
	}
}

// The API key follows the SMTP password's rules exactly: it is never echoed to
// the SPA, so an empty string coming back from the SPA means "no change" and
// only the explicit flag erases it. Getting this backwards would silently
// unconfigure a paid backend on every unrelated save.
func TestBackendSettings_APIKeyWriteRules(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	var none BackendSettings

	key := "sk-store-test"
	if err := s.PutBackendSettings(ctx, BackendUpdate{APIKey: &key}); err != nil {
		t.Fatal(err)
	}
	if got := s.ResolveBackendSettings(ctx, none); got.APIKey != key {
		t.Fatalf("APIKey = %q, want it stored", got.APIKey)
	}

	empty := ""
	if err := s.PutBackendSettings(ctx, BackendUpdate{APIKey: &empty}); err != nil {
		t.Fatal(err)
	}
	if got := s.ResolveBackendSettings(ctx, none); got.APIKey != key {
		t.Errorf("APIKey = %q after an empty update, want it untouched", got.APIKey)
	}

	if err := s.PutBackendSettings(ctx, BackendUpdate{ClearAPIKey: true}); err != nil {
		t.Fatal(err)
	}
	if got := s.ResolveBackendSettings(ctx, none); got.APIKey != "" {
		t.Errorf("APIKey = %q after ClearAPIKey", got.APIKey)
	}
}

// A backend name the process can't build must never be written: it would
// survive a restart and silently resolve to Ollama, summarizing against
// whatever endpoint that happens to be.
func TestBackendSettings_RejectsUnknownBackend(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	bad := "gpt5-please"
	if err := s.PutBackendSettings(ctx, BackendUpdate{Backend: &bad}); err == nil {
		t.Fatal("PutBackendSettings accepted an unknown backend")
	}
	if got := s.ResolveBackendChoice(ctx, BackendOllama); got != BackendOllama {
		t.Errorf("backend = %q after a rejected write", got)
	}
}

// ResolveBackendChoice is what the caller uses to pick which env pair supplies
// the fallback, so it has to be total: an unusable stored value and an unusable
// fallback both land on Ollama rather than on "".
func TestResolveBackendChoice_FallsBackToOllama(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	if got := s.ResolveBackendChoice(ctx, ""); got != BackendOllama {
		t.Errorf("empty fallback = %q, want ollama", got)
	}
	if got := s.ResolveBackendChoice(ctx, BackendAnthropic); got != BackendAnthropic {
		t.Errorf("fallback = %q, want anthropic", got)
	}
	// A row written outside PutBackendSettings (a hand-edited database, an
	// older build) is ignored rather than honored.
	if err := s.PutAppSetting(ctx, keySummarizeBackend, "nonsense"); err != nil {
		t.Fatal(err)
	}
	if got := s.ResolveBackendChoice(ctx, BackendOpenAI); got != BackendOpenAI {
		t.Errorf("with a nonsense row: %q, want the fallback", got)
	}
}

// The Ollama backend owns neither summarize_base_url nor summarize_model — its
// endpoint is EMBER_OLLAMA_URL and its model is the ollama_model row. Storing
// either for Ollama builds NewOllama("<hosted url>", "<hosted model>"): a
// non-nil client, so Configured() is true, the poller's readiness gate never
// fires, and every article ends up stamped 'skipped', which is terminal.
//
// The guard lives at this layer, not only at the API handler, because
// single-site enforcement already failed once in exactly this way: the
// fallback selection was fixed and the bug re-entered through another writer.
func TestPutBackendSettings_RefusesHostedFieldsForOllama(t *testing.T) {
	ctx := context.Background()
	ollama := BackendOllama
	hostedURL := "https://api.groq.com/openai/v1"
	hostedModel := "llama-3.3-70b"

	t.Run("backend named in the update", func(t *testing.T) {
		s := NewTest(t)
		if err := s.PutBackendSettings(ctx, BackendUpdate{
			Backend: &ollama, BaseURL: &hostedURL,
		}); err == nil {
			t.Fatal("PutBackendSettings stored a hosted base URL for the ollama backend")
		}
		if err := s.PutBackendSettings(ctx, BackendUpdate{
			Backend: &ollama, Model: &hostedModel,
		}); err == nil {
			t.Fatal("PutBackendSettings stored a hosted model for the ollama backend")
		}
		// A rejected update writes nothing at all — not even the backend row,
		// which is validated and would otherwise be written first.
		assertNoBackendRows(t, s)
	})

	t.Run("backend resolved from the stored row", func(t *testing.T) {
		// A caller changing only the URL still has to be checked against the
		// backend that will actually be in force.
		s := NewTest(t)
		if err := s.PutBackendSettings(ctx, BackendUpdate{Backend: &ollama}); err != nil {
			t.Fatal(err)
		}
		if err := s.PutBackendSettings(ctx, BackendUpdate{BaseURL: &hostedURL}); err == nil {
			t.Fatal("a URL-only update slipped past the guard for a stored ollama backend")
		}
		if err := s.PutBackendSettings(ctx, BackendUpdate{Model: &hostedModel}); err == nil {
			t.Fatal("a model-only update slipped past the guard for a stored ollama backend")
		}
		if got := s.ResolveBackendSettings(ctx, BackendSettings{}); got.BaseURL != "" || got.Model != "" {
			t.Errorf("rejected updates persisted base_url=%q model=%q", got.BaseURL, got.Model)
		}
	})

	t.Run("empty values and the hosted backends are unaffected", func(t *testing.T) {
		s := NewTest(t)
		empty := ""
		// Blanking is exactly what the API handler does before calling, so the
		// live path must never trip the guard.
		if err := s.PutBackendSettings(ctx, BackendUpdate{
			Backend: &ollama, BaseURL: &empty, Model: &empty,
		}); err != nil {
			t.Fatalf("the handler's own call shape was rejected: %v", err)
		}
		openai := BackendOpenAI
		if err := s.PutBackendSettings(ctx, BackendUpdate{
			Backend: &openai, BaseURL: &hostedURL, Model: &hostedModel,
		}); err != nil {
			t.Fatalf("the guard fired for a hosted backend: %v", err)
		}
		got := s.ResolveBackendSettings(ctx, BackendSettings{})
		if got.BaseURL != hostedURL || got.Model != hostedModel {
			t.Errorf("hosted settings did not round-trip: %+v", got)
		}
	})
}

// assertNoBackendRows fails when any of the four summarize_* rows is set.
func assertNoBackendRows(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	for _, k := range []string{keySummarizeBackend, keySummarizeBaseURL, keySummarizeAPIKey, keySummarizeModel} {
		if v, _ := s.GetAppSetting(ctx, k); v != "" {
			t.Errorf("%s = %q after a rejected update, want unset", k, v)
		}
	}
}
