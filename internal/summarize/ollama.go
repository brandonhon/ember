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
	"time"
)

// Ollama summarizes via Ollama's `/api/generate` endpoint. Output is parsed as
// a JSON array of strings — if the model returns plain text, the implementation
// falls back to splitting on newlines/bullets.
type Ollama struct {
	BaseURL    string
	Model      string
	HTTPClient *http.Client
	Timeout    time.Duration
	// MaxInput caps the text we send (in runes) so we don't blow the context
	// window with very long articles.
	MaxInput int
}

// NewOllama constructs an Ollama summarizer. Both URL and model are required.
func NewOllama(baseURL, model string) *Ollama {
	return &Ollama{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Model:      model,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
		Timeout:    60 * time.Second,
		MaxInput:   8000,
	}
}

// promptTemplate instructs the model to emit a strict JSON array of bullets.
const promptTemplate = `You are an editorial summarizer. Read the article below and produce 3 concise, neutral, factual bullet points. Output ONLY a JSON array of strings, like ["bullet 1","bullet 2","bullet 3"]. No preamble, no markdown, no trailing prose.

TITLE: %s
ARTICLE:
%s`

type generateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type generateResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// Summarize calls Ollama and parses the response into bullets.
func (o *Ollama) Summarize(ctx context.Context, title, text string) ([]string, string, error) {
	if o.BaseURL == "" || o.Model == "" {
		return nil, "", errors.New("summarize: ollama url/model not configured")
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	input := text
	if o.MaxInput > 0 && len([]rune(input)) > o.MaxInput {
		runes := []rune(input)
		input = string(runes[:o.MaxInput])
	}

	body, err := json.Marshal(generateRequest{
		Model:  o.Model,
		Prompt: fmt.Sprintf(promptTemplate, title, input),
		Stream: false,
	})
	if err != nil {
		return nil, o.Model, err
	}

	// One retry on transient errors.
	var lastErr error
	for attempt := range 2 {
		bullets, err := o.tryOnce(ctx, body)
		if err == nil {
			return bullets, o.Model, nil
		}
		lastErr = err
		// Don't retry on context cancellation.
		if ctx.Err() != nil {
			return nil, o.Model, ctx.Err()
		}
		// Small backoff between retries.
		if attempt == 0 {
			select {
			case <-time.After(250 * time.Millisecond):
			case <-ctx.Done():
				return nil, o.Model, ctx.Err()
			}
		}
	}
	return nil, o.Model, lastErr
}

func (o *Ollama) tryOnce(ctx context.Context, body []byte) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("summarize: ollama status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var gr generateResponse
	if err := json.Unmarshal(raw, &gr); err != nil {
		return nil, fmt.Errorf("summarize: decode ollama response: %w", err)
	}
	return parseBullets(gr.Response)
}

// parseBullets accepts the model's raw text and pulls out 3 bullets. It tries
// JSON array parsing first, then falls back to line-by-line.
func parseBullets(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("summarize: empty model output")
	}
	// JSON array form.
	if i := strings.Index(s, "["); i >= 0 {
		if j := strings.LastIndex(s, "]"); j > i {
			var arr []string
			if err := json.Unmarshal([]byte(s[i:j+1]), &arr); err == nil {
				out := cleanBullets(arr)
				if len(out) > 0 {
					return out, nil
				}
			}
		}
	}
	// Fallback: split on lines, strip leading bullet markers.
	lines := strings.Split(s, "\n")
	out := cleanBullets(lines)
	if len(out) == 0 {
		return nil, errors.New("summarize: no bullets parsed from model output")
	}
	return out, nil
}

func cleanBullets(in []string) []string {
	var out []string
	for _, line := range in {
		s := strings.TrimSpace(line)
		s = strings.TrimLeft(s, "-•*0123456789.) \t")
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
		if len(out) >= 5 {
			break
		}
	}
	return out
}
