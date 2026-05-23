package summarize

import (
	"context"
	"fmt"
	"strings"
)

// Noop returns a deterministic, content-derived fake summary. Used in
// development and tests so the rest of the pipeline can run without Ollama.
type Noop struct{}

// Summarize returns three bullets derived from the first sentences of text
// (or the title) so test assertions can be deterministic.
func (Noop) Summarize(_ context.Context, title, text string) ([]string, string, error) {
	source := text
	if source == "" {
		source = title
	}
	// Split into sentences naively.
	parts := strings.FieldsFunc(source, func(r rune) bool {
		return r == '.' || r == '!' || r == '?' || r == '\n'
	})
	var bullets []string
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s == "" {
			continue
		}
		bullets = append(bullets, s)
		if len(bullets) >= 3 {
			break
		}
	}
	for len(bullets) < 3 {
		bullets = append(bullets, fmt.Sprintf("Summary point %d: %s", len(bullets)+1, title))
	}
	return bullets[:3], "noop", nil
}
