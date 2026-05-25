// Package summarize generates short article summaries using a small local LLM
// (Ollama by default). The Summarizer interface lets us swap in a no-op
// implementation for tests or a different backend later.
package summarize

import "context"

// Result is the structured summary returned by a Summarizer.
type Result struct {
	// Paragraph is a 1-2 paragraph editorial lead. May be empty if the model
	// only produced bullets.
	Paragraph string
	// Bullets are 3-5 short factual points.
	Bullets []string
	// Cleaned is the article body with newsletter/podcast/app promos and
	// social-follow lines stripped. Empty when the model didn't return one,
	// in which case the caller should fall back to the original body.
	Cleaned string
}

// Summarizer produces editorial article summaries (lead paragraph + bullets).
type Summarizer interface {
	// Summarize returns a paragraph + bullets and the model name that produced
	// them. Implementations should respect ctx for cancellation and must not
	// panic on malformed model output.
	Summarize(ctx context.Context, title, text string) (Result, string, error)
}
