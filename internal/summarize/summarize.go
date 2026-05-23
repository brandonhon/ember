// Package summarize generates short article summaries using a small local LLM
// (Ollama by default). The Summarizer interface lets us swap in a no-op
// implementation for tests or a different backend later.
package summarize

import "context"

// Summarizer produces bullet-point article summaries.
type Summarizer interface {
	// Summarize returns 3–5 concise bullet strings and the model name that
	// produced them. Implementations should respect ctx for cancellation and
	// must not panic on malformed model output.
	Summarize(ctx context.Context, title, text string) (bullets []string, model string, err error)
}
