package summarize

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestClaude_Summarize_ParsesTextBlock(t *testing.T) {
	var gotPath, gotKey, gotVersion, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5",
			"content":[{"type":"text","text":"SUMMARY: A lead.\n\nPOINTS:\n- one\n- two\n- three"}],
			"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":20}}`)
	}))
	defer srv.Close()

	c := newClaudeWithBaseURL("sk-ant-test", "claude-opus-5", srv.URL)
	res, model, err := c.Summarize(context.Background(), "Title", "Body")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/messages" {
		t.Fatalf("path = %q, want /v1/messages", gotPath)
	}
	if gotKey != "sk-ant-test" || gotVersion == "" {
		t.Fatalf("auth headers: x-api-key=%q anthropic-version=%q", gotKey, gotVersion)
	}
	if model != "claude-opus-5" {
		t.Fatalf("model = %q", model)
	}
	if res.Paragraph != "A lead." || len(res.Bullets) != 3 {
		t.Fatalf("parsed = %+v", res)
	}
	// The instructions go in `system`; the attacker-controlled article goes in
	// a user message. Merging them would put feed content in the trusted half.
	var body map[string]any
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["system"]; !ok {
		t.Fatal("expected a system field carrying the instructions")
	}
	// The SDK's JSON encoder HTML-escapes "<" and ">" to </> (Go's
	// default json.Marshal behavior), so the raw wire bytes never contain a
	// literal "<article>" — decode the user message text before comparing.
	messages, _ := body["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("messages = %+v, want exactly one", messages)
	}
	msg, _ := messages[0].(map[string]any)
	content, _ := msg["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content = %+v, want exactly one block", content)
	}
	block, _ := content[0].(map[string]any)
	text, _ := block["text"].(string)
	if !strings.Contains(text, "<article>") {
		t.Fatalf("expected the fenced <article> block in the user message, got: %s", text)
	}
}

func TestClaude_Summarize_RefusalIsAnError(t *testing.T) {
	// A safety refusal is HTTP 200 with stop_reason "refusal" and no usable
	// text. It must surface as an error so the poller stamps 'skipped' and the
	// article becomes readable, rather than being parsed into an empty summary.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"m","type":"message","role":"assistant",
			"model":"claude-opus-5","content":[],"stop_reason":"refusal",
			"stop_details":{"type":"refusal","category":"cyber"},
			"usage":{"input_tokens":1,"output_tokens":0}}`)
	}))
	defer srv.Close()

	_, _, err := newClaudeWithBaseURL("k", "claude-opus-5", srv.URL).
		Summarize(context.Background(), "t", "b")
	if err == nil || !strings.Contains(err.Error(), "refus") {
		t.Fatalf("err = %v, want a refusal error", err)
	}
}

func TestClaude_NoAPIKeyIsAnError(t *testing.T) {
	if _, _, err := NewClaude("", "claude-opus-5").Summarize(context.Background(), "t", "b"); err == nil {
		t.Fatal("expected an error when no API key is configured")
	}
}

// A malformed HTTP-level failure (bad JSON, non-200 status) must not panic and
// must surface as an error rather than a zero-value Result being mistaken for
// a genuine empty summary.
func TestClaude_Summarize_HTTPErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`)
	}))
	defer srv.Close()

	const secretKey = "sk-ant-do-not-leak-me"
	_, _, err := newClaudeWithBaseURL(secretKey, "claude-opus-5", srv.URL).
		Summarize(context.Background(), "t", "b")
	if err == nil {
		t.Fatal("expected an error on a non-200 response")
	}
	if strings.Contains(err.Error(), secretKey) {
		t.Fatalf("error must not leak the API key: %v", err)
	}
}

// The empty-model case must fall back to DefaultClaudeModel rather than
// sending an empty model id to the API — the admin UI's model field is free
// text and can be cleared.
func TestClaude_EmptyModelFallsBackToDefault(t *testing.T) {
	c := newClaudeWithBaseURL("k", "", "http://example.invalid")
	if got := c.Model(); got != DefaultClaudeModel {
		t.Fatalf("Model() = %q, want %q", got, DefaultClaudeModel)
	}
	c.SetModel("")
	if got := c.Model(); got != DefaultClaudeModel {
		t.Fatalf("after SetModel(\"\"), Model() = %q, want %q", got, DefaultClaudeModel)
	}
}

// SetAPIKey must take effect on the very next call — the admin API mutates the
// key from an HTTP handler while the poller may be about to call Summarize.
func TestClaude_SetAPIKeyTakesEffect(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"m","type":"message","role":"assistant",
			"model":"claude-opus-5","content":[{"type":"text","text":"SUMMARY: ok\n\nPOINTS:\n- a\n- b\n- c"}],
			"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()

	c := newClaudeWithBaseURL("", "claude-opus-5", srv.URL)
	if _, _, err := c.Summarize(context.Background(), "t", "b"); err == nil {
		t.Fatal("expected an error before a key is set")
	}
	c.SetAPIKey("sk-ant-rotated")
	if _, _, err := c.Summarize(context.Background(), "t", "b"); err != nil {
		t.Fatal(err)
	}
	if gotKey != "sk-ant-rotated" {
		t.Fatalf("gotKey = %q, want the rotated key", gotKey)
	}
}

// TestClaude_ConcurrentAdminAndSummarizeDoNotRace mirrors the equivalent
// OpenAI/Ollama test: the admin API mutates model/key from an HTTP handler
// while the poller's summary worker is mid-Summarize. The atomic.Pointer
// client and atomic.Value model/key exist specifically for this race; this
// test only proves their absence under -race, it does not assert on results.
func TestClaude_ConcurrentAdminAndSummarizeDoNotRace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"m","type":"message","role":"assistant",
			"model":"claude-opus-5","content":[{"type":"text","text":"SUMMARY: ok\n\nPOINTS:\n- a\n- b\n- c"}],
			"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()

	c := newClaudeWithBaseURL("sk-ant-test", "claude-opus-5", srv.URL)

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = c.Summarize(context.Background(), "t", "body text")
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.SetModel("claude-opus-5")
			c.SetAPIKey("sk-ant-test")
		}()
	}
	wg.Wait()
}
