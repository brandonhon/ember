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

	res, model, err := o.Summarize(context.Background(), "Hello", "World body content")
	if err != nil {
		t.Fatal(err)
	}
	if model != "qwen2.5:1.5b" {
		t.Errorf("model = %q", model)
	}
	if len(res.Bullets) != 3 {
		t.Fatalf("bullets = %d", len(res.Bullets))
	}
	if !strings.HasPrefix(res.Bullets[0], "First") {
		t.Errorf("first bullet = %q", res.Bullets[0])
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
	res, _, err := o.Summarize(context.Background(), "T", "x")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Bullets) != 3 {
		t.Errorf("got %d bullets: %+v", len(res.Bullets), res.Bullets)
	}
	if res.Bullets[0] != "bullet 1" {
		t.Errorf("stripped bullet wrong: %q", res.Bullets[0])
	}
}

func TestOllama_StripsInlineMarkdownAndQuotes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": `SUMMARY: "A neutral lead."

POINTS:
- **Commit History**: Shows who modified each file
- ` + "`code`" + ` runs fast
- ### Features of Git`,
			"done": true,
		})
	}))
	defer srv.Close()
	o := NewOllama(srv.URL, "m")
	o.HTTPClient = srv.Client()
	res, _, err := o.Summarize(context.Background(), "T", "x")
	if err != nil {
		t.Fatal(err)
	}
	if res.Paragraph != "A neutral lead." {
		t.Errorf("paragraph quoted: %q", res.Paragraph)
	}
	if len(res.Bullets) < 2 {
		t.Fatalf("expected at least 2 bullets, got %+v", res.Bullets)
	}
	if !strings.Contains(res.Bullets[0], "Commit History: Shows") {
		t.Errorf("bold not flattened in first bullet: %q", res.Bullets[0])
	}
	if strings.Contains(res.Bullets[1], "`") {
		t.Errorf("backtick code not flattened: %q", res.Bullets[1])
	}
}

func TestOllama_LabeledFormStripsBoldAndPromptEcho(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			// Realistic qwen2.5:0.5b output: bolded paragraph and a stray
			// "TITLE:**" bullet echoed back from the prompt.
			"response": "**SUMMARY:** **A neutral lead.**\n\nPOINTS:\n- First point\n- Second point\n- TITLE:**\n- Third point",
			"done":     true,
		})
	}))
	defer srv.Close()
	o := NewOllama(srv.URL, "m")
	o.HTTPClient = srv.Client()
	res, _, err := o.Summarize(context.Background(), "T", "x")
	if err != nil {
		t.Fatal(err)
	}
	if res.Paragraph != "A neutral lead." {
		t.Errorf("paragraph not stripped of bold: %q", res.Paragraph)
	}
	wantBullets := []string{"First point", "Second point", "Third point"}
	if len(res.Bullets) != len(wantBullets) {
		t.Fatalf("bullets = %+v, want %v", res.Bullets, wantBullets)
	}
	for i, w := range wantBullets {
		if res.Bullets[i] != w {
			t.Errorf("bullets[%d] = %q, want %q", i, res.Bullets[i], w)
		}
	}
}

func TestOllama_LabeledForm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": "SUMMARY: A short editorial lead about belugas.\n\nPOINTS:\n- Belugas pass the mirror test\n- The test indicates self-awareness\n- Only a few species pass it",
			"done":     true,
		})
	}))
	defer srv.Close()
	o := NewOllama(srv.URL, "m")
	o.HTTPClient = srv.Client()
	res, _, err := o.Summarize(context.Background(), "T", "x")
	if err != nil {
		t.Fatal(err)
	}
	if res.Paragraph != "A short editorial lead about belugas." {
		t.Errorf("paragraph = %q", res.Paragraph)
	}
	if len(res.Bullets) != 3 || res.Bullets[0] != "Belugas pass the mirror test" {
		t.Errorf("bullets = %+v", res.Bullets)
	}
}

func TestOllama_ObjectForm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": `{"paragraph": "A neutral lead.", "bullets": ["one", "two", "three"]}`,
			"done":     true,
		})
	}))
	defer srv.Close()
	o := NewOllama(srv.URL, "m")
	o.HTTPClient = srv.Client()
	res, _, err := o.Summarize(context.Background(), "T", "x")
	if err != nil {
		t.Fatal(err)
	}
	if res.Paragraph != "A neutral lead." {
		t.Errorf("paragraph = %q", res.Paragraph)
	}
	if len(res.Bullets) != 3 || res.Bullets[0] != "one" {
		t.Errorf("bullets = %+v", res.Bullets)
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
	r1, m1, err := n.Summarize(context.Background(), "Title", "First sentence. Second sentence! Third? More.")
	if err != nil {
		t.Fatal(err)
	}
	if m1 != "noop" {
		t.Errorf("model = %q", m1)
	}
	if len(r1.Bullets) != 3 {
		t.Fatalf("bullets = %d", len(r1.Bullets))
	}
	r2, _, _ := n.Summarize(context.Background(), "Title", "First sentence. Second sentence! Third? More.")
	for i := range r1.Bullets {
		if r1.Bullets[i] != r2.Bullets[i] {
			t.Errorf("non-deterministic: %q vs %q", r1.Bullets[i], r2.Bullets[i])
		}
	}
	if r1.Paragraph == "" {
		t.Error("expected non-empty paragraph from noop")
	}
}

func TestNoop_FallsBackToTitle(t *testing.T) {
	n := Noop{}
	r, _, _ := n.Summarize(context.Background(), "Title only", "")
	if len(r.Bullets) != 3 {
		t.Fatalf("bullets = %d", len(r.Bullets))
	}
}
