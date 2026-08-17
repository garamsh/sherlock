package memory_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/memory"
)

// trackingSummarizer records every call and returns a deterministic
// summary. The threshold composite's selection logic — not the actual
// summary quality — is what these tests pin.
type trackingSummarizer struct {
	calls     int
	summary   string
	generated bool
	err       error
}

func (s *trackingSummarizer) Summarize(_ context.Context, _ []string) (memory.Summary, error) {
	s.calls++
	if s.err != nil {
		return memory.Summary{}, s.err
	}
	return memory.Summary{Text: s.summary, Generated: s.generated}, nil
}

func TestThresholdSummarizer_PicksQualityAboveMaxLen(t *testing.T) {
	fast := &trackingSummarizer{summary: "fast"}
	quality := &trackingSummarizer{summary: "quality", generated: true}

	composite := memory.ThresholdSummarizer{
		Fast:    fast,
		Quality: quality,
	}

	out, err := composite.Summarize(context.Background(), []string{"short"})
	require.NoError(t, err)
	assert.Equal(t, "fast", out.Text,
		"below MaxLen the fast summarizer is selected")
	assert.Equal(t, 1, fast.calls)
	assert.Equal(t, 0, quality.calls)

	out, err = composite.Summarize(context.Background(), []string{strings.Repeat("x", memory.SummaryMaxLen+1)})
	require.NoError(t, err)
	assert.Equal(t, "quality", out.Text,
		"above MaxLen the quality summarizer is selected")
	assert.Equal(t, 1, fast.calls)
	assert.Equal(t, 1, quality.calls)
}

// TestThresholdSummarizer_FallsBackToFastOnQualityError pins that a
// quality error does not block consolidation — the concat summarizer
// still produces a coherent summary, so the cluster is never silently
// left un-consolidated because the chat model was down.
func TestThresholdSummarizer_FallsBackToFastOnQualityError(t *testing.T) {
	fast := &trackingSummarizer{summary: "fast"}
	quality := &trackingSummarizer{err: errors.New("provider unavailable")}

	composite := memory.ThresholdSummarizer{
		Fast:    fast,
		Quality: quality,
	}

	out, err := composite.Summarize(context.Background(), []string{strings.Repeat("x", memory.SummaryMaxLen+1)})
	require.NoError(t, err,
		"a quality error falls back to fast, never bubbles up")
	assert.Equal(t, "fast", out.Text)
	assert.Equal(t, 1, quality.calls, "quality is still attempted once")
	assert.Equal(t, 1, fast.calls, "fast picks up after the quality error")
}

func TestThresholdSummarizer_NilQualitySkipsModel(t *testing.T) {
	fast := &trackingSummarizer{summary: "fast"}
	composite := memory.ThresholdSummarizer{Fast: fast}

	out, err := composite.Summarize(context.Background(), []string{strings.Repeat("x", memory.SummaryMaxLen+10)})
	require.NoError(t, err)
	assert.Equal(t, "fast", out.Text,
		"with no quality summarizer wired, fast always wins")
	assert.Equal(t, 1, fast.calls)
}

func TestThresholdSummarizer_FastAtExactlyMaxLen(t *testing.T) {
	fast := &trackingSummarizer{summary: "fast"}
	quality := &trackingSummarizer{summary: "quality", generated: true}
	composite := memory.ThresholdSummarizer{Fast: fast, Quality: quality}

	// Two equal pieces joined by " | " whose total joined length is
	// exactly SummaryMaxLen: the joined text fits one parent without
	// chunking, so the concat summary is already coherent.
	piece := strings.Repeat("x", (memory.SummaryMaxLen-len(memory.ChunkSeparator))/2)
	out, err := composite.Summarize(context.Background(), []string{piece, piece})
	require.NoError(t, err)
	assert.Equal(t, "fast", out.Text,
		"joined length equal to MaxLen still picks fast (concat fits one parent)")
}
