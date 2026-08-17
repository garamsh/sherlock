package memory

import (
	"context"
	"fmt"
	"strings"
)

// SummaryMaxLen caps each compressed semantic parent's content. The
// service.Compress operation splits a cluster's joined summary text into
// multiple parents each at most SummaryMaxLen bytes, so every byte of
// the originals stays inside the searchable index even when the cluster
// is large. A real model-backed summarizer plugs into the same
// Summarizer interface and is subject to the same chunking.
const SummaryMaxLen = 512

// ConcatSummarizer is the deterministic MVP summarizer (ADR 0004): it
// joins the originals' contents and returns the full text without
// truncation. The chunking that keeps every byte searchable lives in
// service.Compress; this summarizer only knows how to flatten inputs.
type ConcatSummarizer struct{}

// NewConcatSummarizer returns the deterministic summarizer.
func NewConcatSummarizer() ConcatSummarizer { return ConcatSummarizer{} }

// Summarize implements Summarizer. The result reports Generated false:
// a join is not a summary, and ADR 0004 makes this a stand-in for a
// model's availability, never for its behavior.
func (ConcatSummarizer) Summarize(_ context.Context, contents []string) (Summary, error) {
	if len(contents) == 0 {
		return Summary{}, fmt.Errorf("summarize: %w: no contents", ErrInvalidInput)
	}
	return Summary{Text: strings.Join(contents, ChunkSeparator)}, nil
}

// ChunkSeparator joins cluster contents inside the deterministic
// summarizer's output and is the boundary the chunker prefers when
// splitting a joined summary across parents.
const ChunkSeparator = " | "
