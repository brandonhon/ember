package summarize

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOllama_SuccessfulJSONArray(t *testing.T) {
	var saw struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
		Stream bool   `json:"stream"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &saw)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": `["First bullet.","Second bullet.","Third bullet."]`,
			"done":     true,
		})
	}))
	defer srv.Close()

	o := NewOllama(srv.URL, "qwen2.5:1.5b")
	o.HTTPClient = srv.Client()

	bullets, model, err := o.Summarize(context.Background(), "Hello", "World body content")
	if err != nil {
		t.Fatal(err)
	}
	if model != "qwen2.5:1.5b" {
		t.Errorf("model = %q", model)
	}
	if len(bullets) != 3 {
		t.Fatalf("bullets = %d", len(bullets))
	}
	if !strings.HasPrefix(bullets[0], "First") {
		t.Errorf("first bullet = %q", bullets[0])
	}
	if saw.Model != "qwen2.5:1.5b" {
		t.Errorf("model not sent: %q", saw.Model)
	}
	if !strings.Contains(saw.Prompt, "Hello") || !strings.Contains(saw.Prompt, "World") {
		t.Errorf("prompt missing inputs: %q", saw.Prompt)
	}
	if saw.Stream {
		t.Error("stream should be false")
	}
}

func TestOllama_FallbackToLineParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": "- bullet 1\n- bullet 2\n* bullet 3\n",
			"done":     true,
		})
	}))
	defer srv.Close()
	o := NewOllama(srv.URL, "m")
	o.HTTPClient = srv.Client()
	bullets, _, err := o.Summarize(context.Background(), "T", "x")
	if err != nil {
		t.Fatal(err)
	}
	if len(bullets) != 3 {
		t.Errorf("got %d bullets: %+v", len(bullets), bullets)
	}
	if bullets[0] != "bullet 1" {
		t.Errorf("stripped bullet wrong: %q", bullets[0])
	}
}

func TestOllama_MalformedResponseFailsGracefully(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": "",
			"done":     true,
		})
	}))
	defer srv.Close()
	o := NewOllama(srv.URL, "m")
	o.HTTPClient = srv.Client()
	_, _, err := o.Summarize(context.Background(), "T", "x")
	if err == nil {
		t.Fatal("expected error on empty model output")
	}
}

func TestOllama_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()
	o := NewOllama(srv.URL, "m")
	o.HTTPClient = srv.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := o.Summarize(ctx, "T", "x")
	if err == nil {
		t.Fatal("expected error on cancelled ctx")
	}
}

func TestOllama_5xxRetryAndError(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	o := NewOllama(srv.URL, "m")
	o.HTTPClient = srv.Client()
	_, _, err := o.Summarize(context.Background(), "T", "x")
	if err == nil {
		t.Fatal("expected 5xx error")
	}
	if hits < 2 {
		t.Errorf("expected at least 2 hits (1 retry), got %d", hits)
	}
}

func TestOllama_TruncatesInput(t *testing.T) {
	var saw struct {
		Prompt string `json:"prompt"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &saw)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": `["a","b","c"]`, "done": true,
		})
	}))
	defer srv.Close()
	o := NewOllama(srv.URL, "m")
	o.HTTPClient = srv.Client()
	o.MaxInput = 10
	long := strings.Repeat("x", 500)
	_, _, err := o.Summarize(context.Background(), "T", long)
	if err != nil {
		t.Fatal(err)
	}
	// Prompt should not contain the full 500 x's.
	if strings.Count(saw.Prompt, "x") > 50 {
		t.Errorf("input not truncated; prompt has %d x's", strings.Count(saw.Prompt, "x"))
	}
}

func TestOllama_MissingConfig(t *testing.T) {
	o := &Ollama{}
	_, _, err := o.Summarize(context.Background(), "T", "x")
	if err == nil {
		t.Error("expected error for missing config")
	}
}

func TestNoop_Deterministic(t *testing.T) {
	n := Noop{}
	b1, m1, err := n.Summarize(context.Background(), "Title", "First sentence. Second sentence! Third? More.")
	if err != nil {
		t.Fatal(err)
	}
	if m1 != "noop" {
		t.Errorf("model = %q", m1)
	}
	if len(b1) != 3 {
		t.Fatalf("bullets = %d", len(b1))
	}
	b2, _, _ := n.Summarize(context.Background(), "Title", "First sentence. Second sentence! Third? More.")
	for i := range b1 {
		if b1[i] != b2[i] {
			t.Errorf("non-deterministic: %q vs %q", b1[i], b2[i])
		}
	}
}

func TestNoop_FallsBackToTitle(t *testing.T) {
	n := Noop{}
	b, _, _ := n.Summarize(context.Background(), "Title only", "")
	if len(b) != 3 {
		t.Fatalf("bullets = %d", len(b))
	}
}
