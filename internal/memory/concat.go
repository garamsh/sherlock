package memory

import (
	"context"
	"fmt"
	"strings"
)

// SummaryMaxLen caps the deterministic summarizer's output length.
const SummaryMaxLen = 512

// ConcatSummarizer is the deterministic MVP summarizer (ADR 0005): it
// joins the originals' contents and truncates. A real model-backed
// summarizer plugs into the same Summarizer interface.
type ConcatSummarizer struct{}

// NewConcatSummarizer returns the deterministic summarizer.
func NewConcatSummarizer() ConcatSummarizer { return ConcatSummarizer{} }

// Summarize implements Summarizer.
func (ConcatSummarizer) Summarize(_ context.Context, contents []string) (string, error) {
	if len(contents) == 0 {
		return "", fmt.Errorf("summarize: %w: no contents", ErrInvalidInput)
	}
	out := strings.Join(contents, " | ")
	if len(out) > SummaryMaxLen {
		out = out[:SummaryMaxLen]
	}
	return out, nil
}
