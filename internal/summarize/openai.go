package summarize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
)

// OpenAI summarizes via any endpoint speaking OpenAI's /v1/chat/completions —
// OpenAI itself, OpenRouter, Groq, Mistral, Gemini's compatibility endpoint,
// vLLM, llama.cpp's server, LiteLLM, and Ollama's own OpenAI shim. It is the de
// facto standard, so one client opens all of them (issue #200).
//
// Model, key and generation options live in atomics for the same reason they do
// on Ollama: the admin API mutates them from HTTP handlers while the poller's
// summary worker is calling Summarize.
type OpenAI struct {
	baseURL    atomic.Value // string
	apiKey     atomic.Value // string
	model      atomic.Value // string
	options    atomic.Pointer[Options]
	HTTPClient *http.Client
	// MaxInput caps the text we send, in runes. Zero means DefaultMaxInput.
	MaxInput int
}

// NewOpenAI constructs the client. apiKey may be empty — a self-hosted vLLM or
// llama.cpp server takes no credential, and sending an empty Bearer breaks some
// of them, so the header is omitted entirely in that case.
func NewOpenAI(baseURL, apiKey, model string) *OpenAI {
	o := &OpenAI{
		HTTPClient: &http.Client{
			// No client Timeout: the poller owns the deadline (issue #201).
			// Redirects are refused for the same reason as Ollama — an
			// admin-set base URL must not bounce a request carrying the API
			// key to somewhere else.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("summarize: unexpected redirect")
			},
		},
		MaxInput: DefaultMaxInput,
	}
	o.baseURL.Store(strings.TrimRight(baseURL, "/"))
	o.apiKey.Store(apiKey)
	o.model.Store(model)
	return o
}

func (o *OpenAI) load(v *atomic.Value) string { s, _ := v.Load().(string); return s }

// Model returns the active model id.
func (o *OpenAI) Model() string { return o.load(&o.model) }

// SetModel atomically swaps the active model.
func (o *OpenAI) SetModel(name string) { o.model.Store(name) }

// SetAPIKey atomically swaps the bearer token. Empty removes the header.
func (o *OpenAI) SetAPIKey(k string) { o.apiKey.Store(k) }

// SetBaseURL atomically swaps the endpoint root.
func (o *OpenAI) SetBaseURL(u string) { o.baseURL.Store(strings.TrimRight(u, "/")) }

// Options returns the active generation tunables.
func (o *OpenAI) Options() Options {
	if p := o.options.Load(); p != nil {
		return *p
	}
	return Options{}
}

// SetOptions atomically swaps the generation tunables.
func (o *OpenAI) SetOptions(opts Options) { o.options.Store(&opts) }

// endpoint returns the chat-completions URL. Providers publish base URLs both
// with and without the version segment (Groq ships ".../openai/v1", OpenAI
// ships "https://api.openai.com" in some docs), so /v1 is appended only when
// it isn't already there.
func (o *OpenAI) endpoint() string {
	base := o.load(&o.baseURL)
	if strings.HasSuffix(base, "/v1") {
		return base + "/chat/completions"
	}
	return base + "/v1/chat/completions"
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature *float64      `json:"temperature,omitempty"`
	TopP        *float64      `json:"top_p,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// Summarize posts one non-streaming chat completion and parses the reply with
// the shared parser. num_ctx has no OpenAI equivalent and is deliberately
// dropped — the server owns the context window.
func (o *OpenAI) Summarize(ctx context.Context, title, text string) (Result, string, error) {
	model := o.Model()
	if o.load(&o.baseURL) == "" || model == "" {
		return Result{}, "", errors.New("summarize: openai url/model not configured")
	}
	maxInput := o.MaxInput
	if maxInput == 0 {
		maxInput = DefaultMaxInput
	}
	req := chatRequest{
		Model:  model,
		Stream: false,
		Messages: []chatMessage{
			{Role: "system", Content: systemInstruction},
			{Role: "user", Content: BuildArticleBlock(title, text, maxInput)},
		},
	}
	if opts := o.Options(); opts.Temperature > 0 || opts.TopP > 0 {
		if opts.Temperature > 0 {
			t := opts.Temperature
			req.Temperature = &t
		}
		if opts.TopP > 0 {
			p := opts.TopP
			req.TopP = &p
		}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return Result{}, model, err
	}
	res, err := o.post(ctx, body)
	if err != nil {
		return Result{}, model, err
	}
	return res, model, nil
}

func (o *OpenAI) post(ctx context.Context, body []byte) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoint(), bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key := o.load(&o.apiKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := o.HTTPClient.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, err
	}
	var cr chatResponse
	// Decode even on a non-200: providers put the actionable message in the
	// body, and an operator staring at "status 429" cannot tell a rate limit
	// from a spend cap.
	_ = json.Unmarshal(raw, &cr)
	if resp.StatusCode != http.StatusOK {
		if cr.Error != nil && cr.Error.Message != "" {
			return Result{}, fmt.Errorf("summarize: openai status %d: %s", resp.StatusCode, cr.Error.Message)
		}
		return Result{}, fmt.Errorf("summarize: openai status %d: %s",
			resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if len(cr.Choices) == 0 {
		return Result{}, errors.New("summarize: openai returned no choices")
	}
	return ParseResult(cr.Choices[0].Message.Content)
}

// ListModels queries GET /v1/models. It is the only model-management operation
// with an OpenAI equivalent — pull and delete are Ollama-only and stay that way
// (issue #200): they don't mean anything for a hosted backend.
func (o *OpenAI) ListModels(ctx context.Context) ([]InstalledModel, error) {
	base := o.load(&o.baseURL)
	if base == "" {
		return nil, errors.New("summarize: openai url not configured")
	}
	url := base + "/v1/models"
	if strings.HasSuffix(base, "/v1") {
		url = base + "/models"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if key := o.load(&o.apiKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := o.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("summarize: openai models status %d", resp.StatusCode)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]InstalledModel, 0, len(body.Data))
	for _, m := range body.Data {
		out = append(out, InstalledModel{Name: m.ID})
	}
	return out, nil
}
