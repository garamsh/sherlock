package memory

import (
	"context"
)

// ThresholdSummarizer is a composite Summarizer that picks between a
// fast (deterministic, no external call) and a quality (model-backed)
// summarizer based on the total joined length of the cluster's
// contents. Below MaxLen the joined text already fits one searchable
// parent so the fast path suffices; above it the quality summarizer is
// asked for a coherent summary the chunker then splits into parents.
// A quality error falls back to fast so a model outage cannot block
// shutdown consolidation.
type ThresholdSummarizer struct {
	Fast    Summarizer
	Quality Summarizer
	// MaxLen is the joined-length cutoff above which Quality is
	// preferred. Zero selects SummaryMaxLen.
	MaxLen int
}

var _ Summarizer = ThresholdSummarizer{}

// Summarize implements Summarizer. Each arm reports its own result, so
// a fallback to Fast carries Generated false however it was reached —
// below the threshold or after a Quality error.
func (t ThresholdSummarizer) Summarize(ctx context.Context, contents []string) (Summary, error) {
	maxLen := t.MaxLen
	if maxLen <= 0 {
		maxLen = SummaryMaxLen
	}
	if t.Quality != nil && joinedLen(contents) > maxLen {
		summary, err := t.Quality.Summarize(ctx, contents)
		if err == nil {
			return summary, nil
		}
	}
	return t.Fast.Summarize(ctx, contents)
}

// joinedLen is the total byte length of contents joined by the
// separator ConcatSummarizer uses: the sum of content lengths plus one
// separator per gap. The threshold lines up with SummaryMaxLen exactly
// because both measure the same joined string.
func joinedLen(contents []string) int {
	if len(contents) == 0 {
		return 0
	}
	n := 0
	for _, c := range contents {
		n += len(c)
	}
	n += len(ChunkSeparator) * (len(contents) - 1)
	return n
}
