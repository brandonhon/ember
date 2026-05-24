package summarize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Ollama summarizes via Ollama's `/api/generate` endpoint. Output is parsed as
// a JSON object with `paragraph` and `bullets` fields. Legacy responses (bare
// JSON arrays, or plain text) are handled by fallback parsers.
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
		HTTPClient: &http.Client{Timeout: 90 * time.Second},
		Timeout:    90 * time.Second,
		MaxInput:   8000,
	}
}

// promptTemplate asks the model for a labeled plain-text format. Small models
// (qwen2.5:1.5b in particular) produce malformed JSON often enough that we
// use a structure they can match more reliably.
const promptTemplate = `You are an editorial summarizer. Read the article below and produce a structured summary.

Format your response EXACTLY like this, with no preamble:

SUMMARY: <one or two neutral sentences summarizing the article>

POINTS:
- <one short factual point>
- <one short factual point>
- <one short factual point>

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

// Summarize calls Ollama and parses the response into a Result.
func (o *Ollama) Summarize(ctx context.Context, title, text string) (Result, string, error) {
	if o.BaseURL == "" || o.Model == "" {
		return Result{}, "", errors.New("summarize: ollama url/model not configured")
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: 90 * time.Second}
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
		return Result{}, o.Model, err
	}

	// One retry on transient errors.
	var lastErr error
	for attempt := range 2 {
		res, err := o.tryOnce(ctx, body)
		if err == nil {
			return res, o.Model, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return Result{}, o.Model, ctx.Err()
		}
		if attempt == 0 {
			select {
			case <-time.After(250 * time.Millisecond):
			case <-ctx.Done():
				return Result{}, o.Model, ctx.Err()
			}
		}
	}
	return Result{}, o.Model, lastErr
}

func (o *Ollama) tryOnce(ctx context.Context, body []byte) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.HTTPClient.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("summarize: ollama status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, err
	}
	var gr generateResponse
	if err := json.Unmarshal(raw, &gr); err != nil {
		return Result{}, fmt.Errorf("summarize: decode ollama response: %w", err)
	}
	return parseResult(gr.Response)
}

// parseResult handles, in order: the labeled "SUMMARY:/POINTS:" format the
// prompt asks for, legacy JSON-object form ({"paragraph":..., "bullets":[...]}),
// legacy bare-array form, and a plain bullet-list fallback.
func parseResult(s string) (Result, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Result{}, errors.New("summarize: empty model output")
	}
	if r, ok := parseLabeled(s); ok {
		return r, nil
	}
	if r, ok := parseJSONObject(s); ok {
		return r, nil
	}
	if r, ok := parseJSONArray(s); ok {
		return r, nil
	}
	// Plain-text fallback: split on lines, strip bullet markers.
	lines := strings.Split(s, "\n")
	out := cleanBullets(lines)
	if len(out) == 0 {
		return Result{}, errors.New("summarize: no bullets parsed from model output")
	}
	return Result{Bullets: out}, nil
}

// parseLabeled handles the prompt's preferred format:
//
//	SUMMARY: <one or two sentences>
//	POINTS:
//	- <point>
//	- <point>
//
// Either section is optional. Markers are case-insensitive. Bullet markers
// accepted: "- ", "* ", "• ", "1. " etc.
func parseLabeled(s string) (Result, bool) {
	// Strip any markdown code fences the model may have added.
	s = strings.TrimSpace(strings.Trim(s, "`"))
	upper := strings.ToUpper(s)
	sumIdx := strings.Index(upper, "SUMMARY:")
	ptsIdx := strings.Index(upper, "POINTS:")
	if sumIdx < 0 && ptsIdx < 0 {
		return Result{}, false
	}
	var paragraph string
	var bulletText string
	switch {
	case sumIdx >= 0 && ptsIdx > sumIdx:
		paragraph = strings.TrimSpace(s[sumIdx+len("SUMMARY:") : ptsIdx])
		bulletText = s[ptsIdx+len("POINTS:"):]
	case sumIdx >= 0:
		paragraph = strings.TrimSpace(s[sumIdx+len("SUMMARY:"):])
	case ptsIdx >= 0:
		bulletText = s[ptsIdx+len("POINTS:"):]
	}
	paragraph = cleanParagraph(paragraph)
	var bullets []string
	if bulletText != "" {
		bullets = cleanBullets(strings.Split(bulletText, "\n"))
	}
	if paragraph == "" && len(bullets) == 0 {
		return Result{}, false
	}
	return Result{Paragraph: paragraph, Bullets: bullets}, true
}

// Inline markdown emphasis patterns. Go's RE2 has no backreferences, so each
// marker gets its own regex; the captured group is the inner text.
var (
	mdBoldRE   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	mdBoldUnRE = regexp.MustCompile(`__([^_]+)__`)
	mdCodeRE   = regexp.MustCompile("`([^`]+)`")
	// Leading "###" / "##" / "#" markdown headings.
	mdHeadingRE = regexp.MustCompile(`^#+\s*`)
)

// cleanParagraph runs each line of the paragraph through the emphasis stripper
// and drops blank/marker-only lines (e.g. standalone "###" separators the
// model emits between sections). Remaining lines are rejoined with single
// newlines so the Reader's "\n{2,}" paragraph split doesn't see fake empty
// paragraphs.
func cleanParagraph(p string) string {
	lines := strings.Split(p, "\n")
	var out []string
	for _, line := range lines {
		s := stripEmphasis(line)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

// stripEmphasis removes markdown emphasis (**, __, *, `, "...") from a string,
// both around the whole string and inline. Models often produce output with
// markdown formatting despite the prompt asking for plain text.
func stripEmphasis(s string) string {
	s = strings.TrimSpace(s)
	s = mdHeadingRE.ReplaceAllString(s, "")
	s = mdBoldRE.ReplaceAllString(s, "$1")
	s = mdBoldUnRE.ReplaceAllString(s, "$1")
	s = mdCodeRE.ReplaceAllString(s, "$1")
	// Iteratively strip outer pairs of markers and surrounding quotes.
	for {
		original := s
		for _, m := range []string{"**", "__", "*", "`", `"`, "'"} {
			s = strings.TrimSpace(strings.TrimPrefix(s, m))
			s = strings.TrimSpace(strings.TrimSuffix(s, m))
		}
		if s == original {
			break
		}
	}
	return s
}

func parseJSONObject(s string) (Result, bool) {
	i := strings.Index(s, "{")
	j := strings.LastIndex(s, "}")
	if i < 0 || j <= i {
		return Result{}, false
	}
	var obj struct {
		Paragraph string   `json:"paragraph"`
		Bullets   []string `json:"bullets"`
	}
	if err := json.Unmarshal([]byte(s[i:j+1]), &obj); err != nil {
		return Result{}, false
	}
	bullets := cleanBullets(obj.Bullets)
	paragraph := strings.TrimSpace(obj.Paragraph)
	if paragraph == "" && len(bullets) == 0 {
		return Result{}, false
	}
	return Result{Paragraph: paragraph, Bullets: bullets}, true
}

func parseJSONArray(s string) (Result, bool) {
	i := strings.Index(s, "[")
	j := strings.LastIndex(s, "]")
	if i < 0 || j <= i {
		return Result{}, false
	}
	var arr []string
	if err := json.Unmarshal([]byte(s[i:j+1]), &arr); err != nil {
		return Result{}, false
	}
	out := cleanBullets(arr)
	if len(out) == 0 {
		return Result{}, false
	}
	return Result{Bullets: out}, true
}

// labelPrefixRE matches "POINT 1:", "Fact 2.", "Point 3 -", etc. — small
// models add their own labels even when asked for plain bullets.
var labelPrefixRE = regexp.MustCompile(`(?i)^\**\s*(?:POINT|FACT|KEY)\s*\d*\s*[:.\-)]?\s*\**\s*`)

func cleanBullets(in []string) []string {
	var out []string
	for _, line := range in {
		s := strings.TrimSpace(line)
		// Strip inline markdown FIRST so a leading "**bold**" doesn't get
		// half-eaten by the bullet-marker trim below.
		s = stripEmphasis(s)
		s = strings.TrimLeft(s, "-•0123456789.) \t")
		s = stripEmphasis(s)
		s = labelPrefixRE.ReplaceAllString(s, "")
		s = stripEmphasis(s)
		if s == "" {
			continue
		}
		if isPromptEcho(s) {
			continue
		}
		out = append(out, s)
		if len(out) >= 5 {
			break
		}
	}
	return out
}

func isPromptEcho(s string) bool {
	u := strings.ToUpper(strings.TrimSpace(s))
	for _, p := range []string{"TITLE:", "ARTICLE:", "SUMMARY:", "POINTS:"} {
		if strings.HasPrefix(u, p) {
			return true
		}
	}
	return false
}
