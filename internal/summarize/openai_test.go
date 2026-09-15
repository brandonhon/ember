package summarize

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestOpenAI_Summarize_SendsChatCompletionsWithBearer(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"SUMMARY: A lead.\n\nPOINTS:\n- one\n- two\n- three"}}]}`)
	}))
	defer srv.Close()

	o := NewOpenAI(srv.URL, "sk-test", "gpt-oss:20b")
	res, model, err := o.Summarize(context.Background(), "Title", "Body")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q, want /v1/chat/completions", gotPath)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("auth = %q, want \"Bearer sk-test\"", gotAuth)
	}
	if model != "gpt-oss:20b" {
		t.Fatalf("model = %q", model)
	}
	if res.Paragraph != "A lead." || len(res.Bullets) != 3 {
		t.Fatalf("parsed = %+v", res)
	}
	// The article payload must be a user message and the instructions a system
	// message — a chat backend that gets both in one blob follows instructions
	// embedded in the article more readily.
	var body struct {
		Messages []struct{ Role, Content string } `json:"messages"`
	}
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Messages) != 2 || body.Messages[0].Role != "system" || body.Messages[1].Role != "user" {
		t.Fatalf("messages = %+v, want [system, user]", body.Messages)
	}
	if !strings.Contains(body.Messages[1].Content, "<article>") {
		t.Fatal("user message should carry the fenced <article> block")
	}
}

func TestOpenAI_Summarize_NoKeySendsNoAuthHeader(t *testing.T) {
	// Local vLLM / llama.cpp servers take no key; sending an empty Bearer is
	// rejected by some of them.
	var sawAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawAuth = r.Header["Authorization"]
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"SUMMARY: x"}}]}`)
	}))
	defer srv.Close()

	if _, _, err := NewOpenAI(srv.URL, "", "m").Summarize(context.Background(), "t", "b"); err != nil {
		t.Fatal(err)
	}
	if sawAuth {
		t.Fatal("no API key configured, so no Authorization header should be sent")
	}
}

func TestOpenAI_Summarize_SurfacesErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"rate limit exceeded"}}`)
	}))
	defer srv.Close()

	_, _, err := NewOpenAI(srv.URL, "sk-secret", "m").Summarize(context.Background(), "t", "b")
	if err == nil || !strings.Contains(err.Error(), "rate limit exceeded") {
		t.Fatalf("err = %v; the operator needs the provider's message to act on it", err)
	}
	// The key must never appear in the surfaced error, even though the
	// request that produced it carried one.
	if strings.Contains(err.Error(), "sk-secret") {
		t.Fatalf("error leaked the API key: %v", err)
	}
}

func TestOpenAI_BaseURLWithVersionSuffixIsNotDoubled(t *testing.T) {
	// Providers publish base URLs both ways; "https://api.groq.com/openai/v1"
	// must not become ".../v1/v1/chat/completions".
	o := NewOpenAI("https://example.test/openai/v1/", "k", "m")
	if got := o.endpoint(); got != "https://example.test/openai/v1/chat/completions" {
		t.Fatalf("endpoint = %q", got)
	}
	o2 := NewOpenAI("https://example.test", "k", "m")
	if got := o2.endpoint(); got != "https://example.test/v1/chat/completions" {
		t.Fatalf("endpoint = %q", got)
	}
}

// TestOpenAI_Summarize_NonOKUnparseableBodySurfacesRawBody proves that when a
// broken endpoint returns a non-200 with a body that isn't the OpenAI error
// shape (or isn't JSON at all), we still surface something actionable — the
// raw body — instead of silently swallowing it.
func TestOpenAI_Summarize_NonOKUnparseableBodySurfacesRawBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "upstream connection reset")
	}))
	defer srv.Close()

	_, _, err := NewOpenAI(srv.URL, "k", "m").Summarize(context.Background(), "t", "b")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "upstream connection reset") {
		t.Fatalf("err = %v, want raw body surfaced", err)
	}
	if !strings.Contains(err.Error(), "502") {
		t.Fatalf("err = %v, want status code surfaced", err)
	}
}

// TestOpenAI_Summarize_ZeroChoicesErrors proves a 200 with an empty choices
// array is treated as an error rather than returning a blank Result.
func TestOpenAI_Summarize_ZeroChoicesErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[]}`)
	}))
	defer srv.Close()

	_, _, err := NewOpenAI(srv.URL, "k", "m").Summarize(context.Background(), "t", "b")
	if err == nil {
		t.Fatal("want error for zero choices")
	}
	if !strings.Contains(err.Error(), "no choices") {
		t.Fatalf("err = %v, want it to mention no choices", err)
	}
}

// TestOpenAI_Summarize_MissingURLOrModelErrors covers the guard clause that
// refuses to build a request at all when the backend isn't configured yet
// (e.g. before the admin has picked a model).
func TestOpenAI_Summarize_MissingURLOrModelErrors(t *testing.T) {
	if _, _, err := NewOpenAI("", "k", "m").Summarize(context.Background(), "t", "b"); err == nil {
		t.Fatal("want error for empty base URL")
	}
	if _, _, err := NewOpenAI("http://example.test", "k", "").Summarize(context.Background(), "t", "b"); err == nil {
		t.Fatal("want error for empty model")
	}
}

// TestOpenAI_Summarize_ForwardsTemperatureAndTopPButDropsNumCtx proves only
// temperature/top_p cross the wire (and only when non-zero), and that
// num_ctx — which has no OpenAI equivalent — is never sent even though
// Options carries it for the Ollama backend.
func TestOpenAI_Summarize_ForwardsTemperatureAndTopPButDropsNumCtx(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"SUMMARY: x"}}]}`)
	}))
	defer srv.Close()

	o := NewOpenAI(srv.URL, "k", "m")
	o.SetOptions(Options{Temperature: 0.5, TopP: 0.9, NumCtx: 4096})
	if _, _, err := o.Summarize(context.Background(), "t", "b"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, `"temperature":0.5`) {
		t.Fatalf("body = %s, want temperature forwarded", gotBody)
	}
	if !strings.Contains(gotBody, `"top_p":0.9`) {
		t.Fatalf("body = %s, want top_p forwarded", gotBody)
	}
	if strings.Contains(gotBody, "num_ctx") {
		t.Fatalf("body = %s, num_ctx has no OpenAI equivalent and must not be sent", gotBody)
	}
}

// TestOpenAI_Summarize_ZeroOptionsOmitsFields proves zero-valued options are
// omitted entirely rather than sent as literal 0s, matching Ollama's "let the
// server pick its default" behavior.
func TestOpenAI_Summarize_ZeroOptionsOmitsFields(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"SUMMARY: x"}}]}`)
	}))
	defer srv.Close()

	o := NewOpenAI(srv.URL, "k", "m")
	if _, _, err := o.Summarize(context.Background(), "t", "b"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gotBody, "temperature") || strings.Contains(gotBody, "top_p") {
		t.Fatalf("body = %s, want no temperature/top_p when unset", gotBody)
	}
}

// TestOpenAI_SetModelSetAPIKeySetOptions_RoundTrip proves the getters observe
// what the setters store — the admin API relies on this for its "current
// config" display.
func TestOpenAI_SetModelSetAPIKeySetOptions_RoundTrip(t *testing.T) {
	o := NewOpenAI("http://example.test", "k1", "model-a")
	if o.Model() != "model-a" {
		t.Fatalf("Model() = %q", o.Model())
	}
	o.SetModel("model-b")
	if o.Model() != "model-b" {
		t.Fatalf("Model() after SetModel = %q", o.Model())
	}
	o.SetAPIKey("k2")
	// SetAPIKey has no getter (the key must not be readable back out through
	// the admin API surface), so we only prove it changes wire behavior —
	// covered by the bearer-header tests above.
	if got := o.Options(); got != (Options{}) {
		t.Fatalf("Options() default = %+v, want zero value", got)
	}
	o.SetOptions(Options{Temperature: 0.7})
	if got := o.Options(); got.Temperature != 0.7 {
		t.Fatalf("Options() after SetOptions = %+v", got)
	}
	o.SetBaseURL("http://other.test/")
	if got := o.endpoint(); got != "http://other.test/v1/chat/completions" {
		t.Fatalf("endpoint after SetBaseURL = %q", got)
	}
}

// TestOpenAI_Summarize_RefusesRedirect proves the client does not follow a
// redirect — the request carries the bearer token, and an admin-set base URL
// that redirects could bounce it to another host.
func TestOpenAI_Summarize_RefusesRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://elsewhere.invalid/", http.StatusFound)
	}))
	defer srv.Close()

	_, _, err := NewOpenAI(srv.URL, "sk-secret", "m").Summarize(context.Background(), "t", "b")
	if err == nil {
		t.Fatal("want error: redirect should be refused")
	}
	if strings.Contains(err.Error(), "sk-secret") {
		t.Fatalf("error leaked the API key: %v", err)
	}
}

func TestOpenAI_ListModels(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"data":[{"id":"gpt-4o-mini"},{"id":"gpt-oss:20b"}]}`)
	}))
	defer srv.Close()

	models, err := NewOpenAI(srv.URL, "sk-list", "m").ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/models" {
		t.Fatalf("path = %q, want /v1/models", gotPath)
	}
	if gotAuth != "Bearer sk-list" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if len(models) != 2 || models[0].Name != "gpt-4o-mini" || models[1].Name != "gpt-oss:20b" {
		t.Fatalf("models = %+v", models)
	}
}

// TestOpenAI_ListModels_BaseURLWithV1SuffixIsNotDoubled mirrors the
// chat-completions endpoint test for the /v1/models path.
func TestOpenAI_ListModels_BaseURLWithV1SuffixIsNotDoubled(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	defer srv.Close()

	o := NewOpenAI(srv.URL+"/v1", "", "m")
	if _, err := o.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/models" {
		t.Fatalf("path = %q, want /v1/models (not /v1/v1/models)", gotPath)
	}
}

func TestOpenAI_ListModels_NoKeySendsNoAuthHeader(t *testing.T) {
	var sawAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawAuth = r.Header["Authorization"]
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	defer srv.Close()

	if _, err := NewOpenAI(srv.URL, "", "m").ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sawAuth {
		t.Fatal("no API key configured, so no Authorization header should be sent")
	}
}

func TestOpenAI_ListModels_URLNotConfigured(t *testing.T) {
	if _, err := NewOpenAI("", "k", "m").ListModels(context.Background()); err == nil {
		t.Fatal("want error when base URL is empty")
	}
}

func TestOpenAI_ListModels_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := NewOpenAI(srv.URL, "sk-secret", "m").ListModels(context.Background())
	if err == nil {
		t.Fatal("want error for non-200")
	}
	if strings.Contains(err.Error(), "sk-secret") {
		t.Fatalf("error leaked the API key: %v", err)
	}
}

// TestOpenAI_Summarize_RespectsContextCancellation proves Summarize doesn't
// swallow ctx cancellation into some other error.
func TestOpenAI_Summarize_RespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"SUMMARY: x"}}]}`)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := NewOpenAI(srv.URL, "k", "m").Summarize(ctx, "t", "b")
	if err == nil {
		t.Fatal("want error for cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// TestOpenAI_ConcurrentAdminAndSummarizeDoNotRace mirrors the equivalent
// Ollama test: model/key/options/base URL are mutated by admin HTTP handlers
// while the poller's summary worker is mid-Summarize. Run with -race.
func TestOpenAI_ConcurrentAdminAndSummarizeDoNotRace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"m"}]}`)
		default:
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"SUMMARY: ok\n\nPOINTS:\n- a\n- b\n- c"}}]}`)
		}
	}))
	defer srv.Close()

	o := NewOpenAI(srv.URL, "sk-test", "m")

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = o.Summarize(context.Background(), "t", "body text")
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = o.ListModels(context.Background())
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			o.SetModel("m")
			o.SetAPIKey("sk-test")
			o.SetOptions(Options{Temperature: 0.5})
			o.SetBaseURL(srv.URL)
		}()
	}
	wg.Wait()
}
