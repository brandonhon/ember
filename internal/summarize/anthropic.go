package summarize

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// DefaultClaudeModel is what a fresh Claude backend starts on. Free text in the
// UI rather than a picker: Anthropic ships model ids faster than Ember ships
// releases, and pinning a list here would date the feature.
const DefaultClaudeModel = "claude-opus-5"

// claudeMaxTokens bounds the reply. A summary is a lead paragraph, three
// bullets and a promo-stripped body, so this is generous; it is not a cost
// control, it stops a runaway generation.
const claudeMaxTokens = 16000

// Claude summarizes via Anthropic's Messages API using the official Go SDK.
//
// It is a third transport onto the shared prompt: systemInstruction goes in
// `system`, the fenced <article> block goes in a user message, and the reply
// runs through the same ParseResult as Ollama's. Splitting the two halves
// matters more here than on /api/generate — feed content is attacker-controlled
// and belongs in the untrusted half of the request.
type Claude struct {
	client atomic.Pointer[anthropic.Client]
	apiKey atomic.Value // string
	model  atomic.Value // string
	// baseURL is empty in production (the SDK's default) and set by tests.
	baseURL string
	// MaxInput caps the article text in runes. Zero means DefaultMaxInput.
	MaxInput int
}

// NewClaude constructs the backend. An empty key is allowed at construction —
// Summarize reports it, so an admin can select the backend and fill the key in
// afterwards without the process refusing to start.
func NewClaude(apiKey, model string) *Claude {
	return newClaudeWithBaseURL(apiKey, model, "")
}

func newClaudeWithBaseURL(apiKey, model, baseURL string) *Claude {
	if model == "" {
		model = DefaultClaudeModel
	}
	c := &Claude{baseURL: baseURL, MaxInput: DefaultMaxInput}
	c.apiKey.Store(apiKey)
	c.model.Store(model)
	c.rebuild(apiKey)
	return c
}

// rebuild swaps in a client carrying the new key. The SDK client is immutable
// once constructed, so a key change means a new client — stored in an atomic
// for the same reason Ollama's model is: the admin API mutates it from an HTTP
// handler while the summary worker is mid-call.
func (c *Claude) rebuild(apiKey string) {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if c.baseURL != "" {
		opts = append(opts, option.WithBaseURL(c.baseURL))
	}
	cl := anthropic.NewClient(opts...)
	c.client.Store(&cl)
}

// Model returns the active model id.
func (c *Claude) Model() string { s, _ := c.model.Load().(string); return s }

// SetModel atomically swaps the active model.
func (c *Claude) SetModel(name string) {
	if name == "" {
		name = DefaultClaudeModel
	}
	c.model.Store(name)
}

// SetAPIKey atomically swaps the credential.
func (c *Claude) SetAPIKey(k string) {
	c.apiKey.Store(k)
	c.rebuild(k)
}

// Summarize sends one non-streaming Messages request and parses the reply.
func (c *Claude) Summarize(ctx context.Context, title, text string) (Result, string, error) {
	model := c.Model()
	key, _ := c.apiKey.Load().(string)
	if key == "" {
		return Result{}, model, errors.New("summarize: anthropic api key not configured")
	}
	maxInput := c.MaxInput
	if maxInput == 0 {
		maxInput = DefaultMaxInput
	}
	cl := c.client.Load()
	if cl == nil {
		return Result{}, model, errors.New("summarize: anthropic client not initialised")
	}

	resp, err := cl.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: claudeMaxTokens,
		System: []anthropic.TextBlockParam{{
			Text: systemInstruction,
		}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(
				anthropic.NewTextBlock(BuildArticleBlock(title, text, maxInput)),
			),
		},
		// A one-paragraph article summary needs no deep reasoning. Effort is
		// the documented lever for that — NOT thinking:{type:"disabled"},
		// which on Opus 5 can leak reasoning into the visible text that
		// ParseResult would then try to read as the summary. We deliberately
		// leave Thinking unset (SDK default) and only turn this dial down.
		OutputConfig: anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffortLow,
		},
	})
	if err != nil {
		// The SDK's error already carries the status/body; it does not fold
		// in our RequestOptions (and therefore not the API key), so wrapping
		// it here does not leak the credential.
		return Result{}, model, fmt.Errorf("summarize: anthropic request failed: %w", err)
	}
	// A safety classifier can decline with HTTP 200 and no usable content.
	// Report it as an error so the poller stamps 'skipped' and the article
	// becomes readable, instead of parsing empty text into an empty summary.
	if resp.StopReason == anthropic.StopReasonRefusal {
		return Result{}, model, fmt.Errorf("summarize: anthropic refused the request (category %q)",
			resp.StopDetails.Category)
	}
	var out string
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			out += tb.Text
		}
	}
	res, err := ParseResult(out)
	return res, model, err
}
