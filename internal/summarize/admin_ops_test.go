package summarize

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// captured records what the fake Ollama daemon received.
type captured struct {
	method atomic.Value // string
	path   atomic.Value // string
	body   atomic.Value // string
}

func fakeOllama(t *testing.T, status int, respBody string) (*Ollama, *captured) {
	t.Helper()
	c := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c.method.Store(r.Method)
		c.path.Store(r.URL.Path)
		c.body.Store(string(b))
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	return NewOllama(srv.URL, "m"), c
}

func str(v atomic.Value) string {
	s, _ := v.Load().(string)
	return s
}

// Delete issues DELETE /api/delete with the model name and treats 200 as success.
func TestDelete_RequestShapeAndSuccess(t *testing.T) {
	o, c := fakeOllama(t, http.StatusOK, `{}`)
	if err := o.Delete(context.Background(), "qwen2.5:0.5b"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got := str(c.method); got != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", got)
	}
	if got := str(c.path); got != "/api/delete" {
		t.Errorf("path = %s, want /api/delete", got)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(str(c.body)), &payload); err != nil {
		t.Fatalf("body is not JSON: %q", str(c.body))
	}
	if payload["name"] != "qwen2.5:0.5b" {
		t.Errorf("payload = %v, want name=qwen2.5:0.5b", payload)
	}
}

// Pull posts to /api/pull with stream disabled — the handler blocks on
// completion, so a streaming response would never terminate.
func TestPull_RequestShapeAndSuccess(t *testing.T) {
	o, c := fakeOllama(t, http.StatusOK, `{"status":"success"}`)
	if err := o.Pull(context.Background(), "llama3"); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if got := str(c.method); got != http.MethodPost {
		t.Errorf("method = %s, want POST", got)
	}
	if got := str(c.path); got != "/api/pull" {
		t.Errorf("path = %s, want /api/pull", got)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(str(c.body)), &payload); err != nil {
		t.Fatalf("body is not JSON: %q", str(c.body))
	}
	if payload["name"] != "llama3" {
		t.Errorf("payload name = %v, want llama3", payload["name"])
	}
	if payload["stream"] != false {
		t.Errorf("payload stream = %v, want false (Pull blocks until done)", payload["stream"])
	}
}

// A refusal must surface Ollama's own explanation — that text is what the
// admin UI shows when a pull or delete is rejected.
func TestDeleteAndPull_SurfaceOllamasReason(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*Ollama) error
		want string
	}{
		{"delete", func(o *Ollama) error { return o.Delete(context.Background(), "nope") }, "delete status 404"},
		{"pull", func(o *Ollama) error { return o.Pull(context.Background(), "nope") }, "pull status 404"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, _ := fakeOllama(t, http.StatusNotFound, `{"error":"model 'nope' not found"}`)
			err := tc.call(o)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), "model 'nope' not found") {
				t.Errorf("error = %q, want it to carry Ollama's own message", err)
			}
		})
	}
}

// statusError bounds the snippet it embeds so a hostile or broken daemon
// can't push an unbounded body into an error string (and a log line).
func TestStatusError_BoundsTheBodySnippet(t *testing.T) {
	huge := strings.Repeat("A", 100_000)
	resp := &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(huge))}
	err := statusError("pull", resp)
	if err == nil {
		t.Fatal("expected an error")
	}
	if n := len(err.Error()); n > 4096+128 {
		t.Errorf("error is %d bytes; the body snippet should be capped near 4096", n)
	}
	if !strings.Contains(err.Error(), "pull status 500") {
		t.Errorf("error = %q, want the op and status", err)
	}
}

// Every remote call refuses when no base URL is configured, before any I/O.
func TestAdminOps_RequireBaseURL(t *testing.T) {
	o := &Ollama{}
	o.SetModel("m")
	if _, err := o.ListInstalled(context.Background()); err == nil {
		t.Error("ListInstalled accepted an empty base URL")
	}
	if err := o.Delete(context.Background(), "m"); err == nil {
		t.Error("Delete accepted an empty base URL")
	}
	if err := o.Pull(context.Background(), "m"); err == nil {
		t.Error("Pull accepted an empty base URL")
	}
	if _, _, err := o.Summarize(context.Background(), "t", "x"); err == nil {
		t.Error("Summarize accepted an empty base URL")
	}
}

func TestOptions_RoundTripAndClear(t *testing.T) {
	o := NewOllama("http://x.test", "m")
	if got := o.Options(); got != (Options{}) {
		t.Errorf("fresh Options = %+v, want zero", got)
	}
	want := Options{Temperature: 0.4, TopP: 0.9, NumCtx: 4096}
	o.SetOptions(want)
	if got := o.Options(); got != want {
		t.Errorf("Options = %+v, want %+v", got, want)
	}
	o.SetOptions(Options{})
	if got := o.Options(); got != (Options{}) {
		t.Errorf("Options after clear = %+v, want zero", got)
	}
}

// Tunables are only sent when set — a zero value must mean "let Ollama pick",
// not "force 0", which would pin temperature to fully deterministic output.
func TestSummarize_OmitsUnsetOptions(t *testing.T) {
	o, c := fakeOllama(t, http.StatusOK, `{"response":"SUMMARY: s\n\nPOINTS:\n- a","done":true}`)
	if _, _, err := o.Summarize(context.Background(), "t", "body"); err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	var req map[string]any
	if err := json.Unmarshal([]byte(str(c.body)), &req); err != nil {
		t.Fatal(err)
	}
	if _, present := req["options"]; present {
		t.Errorf("unset tunables were sent: %v", req["options"])
	}

	o.SetOptions(Options{Temperature: 0.4})
	if _, _, err := o.Summarize(context.Background(), "t", "body"); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(str(c.body)), &req)
	opts, ok := req["options"].(map[string]any)
	if !ok {
		t.Fatalf("options missing after SetOptions: %v", req)
	}
	if opts["temperature"] != 0.4 {
		t.Errorf("temperature = %v, want 0.4", opts["temperature"])
	}
	if _, present := opts["top_p"]; present {
		t.Errorf("unset top_p was sent: %v", opts)
	}
}

// Summarize retries once on a transient failure. A daemon that is briefly
// unhappy shouldn't cost the article its summary.
func TestSummarize_RetriesOnceThenSucceeds(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, `{"response":"SUMMARY: recovered\n\nPOINTS:\n- a","done":true}`)
	}))
	defer srv.Close()

	o := NewOllama(srv.URL, "m")
	res, model, err := o.Summarize(context.Background(), "t", "body")
	if err != nil {
		t.Fatalf("Summarize did not retry: %v", err)
	}
	if model != "m" || res.Paragraph != "recovered" {
		t.Errorf("res = %+v model = %q", res, model)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("made %d calls, want exactly 2 (one retry)", got)
	}
}

// ...but only once: a persistently failing daemon must give up, not spin.
func TestSummarize_GivesUpAfterTheRetry(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, _, err := NewOllama(srv.URL, "m").Summarize(context.Background(), "t", "body"); err == nil {
		t.Fatal("expected an error from a persistently failing daemon")
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("made %d calls, want exactly 2", got)
	}
}

// A cancelled context stops immediately rather than burning the retry.
func TestSummarize_CancelledContextDoesNotRetry(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := NewOllama(srv.URL, "m").Summarize(ctx, "t", "body"); err == nil {
		t.Fatal("expected a context error")
	}
	if got := calls.Load(); got > 1 {
		t.Errorf("made %d calls on a cancelled context, want at most 1", got)
	}
}

// MaxInput truncates by RUNES, not bytes — byte-slicing multibyte text would
// send invalid UTF-8 to the model.
func TestSummarize_TruncatesByRunes(t *testing.T) {
	o, c := fakeOllama(t, http.StatusOK, `{"response":"SUMMARY: s","done":true}`)
	o.MaxInput = 5
	body := strings.Repeat("日", 50) // 3 bytes each
	if _, _, err := o.Summarize(context.Background(), "t", body); err != nil {
		t.Fatal(err)
	}
	var req struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal([]byte(str(c.body)), &req); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(req.Prompt, "日"); got != 5 {
		t.Errorf("sent %d runes of body, want 5", got)
	}
	if !strings.Contains(req.Prompt, "日日日日日") {
		t.Errorf("truncation produced broken text: %q", req.Prompt)
	}
}

func TestListInstalled_ErrorPaths(t *testing.T) {
	o, _ := fakeOllama(t, http.StatusInternalServerError, `boom`)
	if _, err := o.ListInstalled(context.Background()); err == nil {
		t.Error("non-200 accepted")
	}
	o2, _ := fakeOllama(t, http.StatusOK, `{not json`)
	if _, err := o2.ListInstalled(context.Background()); err == nil {
		t.Error("malformed JSON accepted")
	}
}
