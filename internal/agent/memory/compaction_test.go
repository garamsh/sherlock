package memory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	agentmemory "github.com/garamsh/gagent/internal/agent/memory"
	"github.com/garamsh/gagent/internal/memory"
)

// fixedSummarizer returns one prepared result, so a test states what
// the summarizer produced rather than which implementation it wired.
type fixedSummarizer struct {
	summary memory.Summary
	err     error
	calls   int
}

func (s *fixedSummarizer) Summarize(_ context.Context, _ []string) (memory.Summary, error) {
	s.calls++
	return s.summary, s.err
}

// recordingStore records what compaction asked to store. Only Store is
// reached by this path; the rest of memory.Service is unimplemented on
// purpose, so a compactor that started writing elsewhere would panic
// rather than pass.
type recordingStore struct {
	memory.Service
	stored []memory.StoreInput
}

func (s *recordingStore) Store(_ context.Context, in memory.StoreInput) (memory.StoreResult, error) {
	s.stored = append(s.stored, in)
	return memory.StoreResult{ID: int64(len(s.stored))}, nil
}

// history returns n user messages with distinguishable content.
func history(n int) []agent.Message {
	out := make([]agent.Message, 0, n)
	for i := range n {
		out = append(out, agent.Message{Role: agent.RoleUser, Content: string(rune('a' + i%26))})
	}
	return out
}

// TestCompact_StoresNothingWhenTheSummarizerDidNotSummarize pins the
// rule ADR 0006 rests the context-overflow write on: the write is
// licensed by the content being model-generated, so a deterministic
// stand-in's join — which is the evicted turns themselves — is not
// stored. Before this rule the shipped default stored every evicted
// user message verbatim (#541).
func TestCompact_StoresNothingWhenTheSummarizerDidNotSummarize(t *testing.T) {
	store := &recordingStore{}
	summarizer := &fixedSummarizer{summary: memory.Summary{Text: "a | b | c"}}
	compactor := agentmemory.Compactor{Svc: store, Summarizer: summarizer, MaxHistoryMessages: 2}

	kept, err := compactor.Compact(context.Background(), history(6))

	require.NoError(t, err)
	assert.Empty(t, store.stored,
		"a join is not a summary, so the evicted turns do not enter the searchable store")
	assert.Len(t, kept, 2,
		"the turns still leave the window: this changes what is stored, not what is evicted")
	assert.Equal(t, 1, summarizer.calls)
}

// TestCompact_StoresWhatAModelSummarized pins the other half: a real
// summarizer's output is left alone and stored as the episodic memory
// ADR 0006 describes.
func TestCompact_StoresWhatAModelSummarized(t *testing.T) {
	store := &recordingStore{}
	summarizer := &fixedSummarizer{
		summary: memory.Summary{Text: "the session discussed deployment", Generated: true},
	}
	compactor := agentmemory.Compactor{Svc: store, Summarizer: summarizer, MaxHistoryMessages: 2}

	kept, err := compactor.Compact(context.Background(), history(6))

	require.NoError(t, err)
	require.Len(t, store.stored, 1)
	assert.Equal(t, "the session discussed deployment", store.stored[0].Content)
	assert.Equal(t, memory.TypeEpisodic, store.stored[0].Type)
	assert.Equal(t, "context-overflow", store.stored[0].Source)
	assert.Len(t, kept, 2)
}

// TestCompact_SummarizerErrorFails pins that a summarizer error is not
// the same as a summarizer that did not summarize: one is a failure and
// surfaces, the other is a decision not to store.
func TestCompact_SummarizerErrorFails(t *testing.T) {
	store := &recordingStore{}
	summarizer := &fixedSummarizer{err: errors.New("provider unavailable")}
	compactor := agentmemory.Compactor{Svc: store, Summarizer: summarizer, MaxHistoryMessages: 2}

	_, err := compactor.Compact(context.Background(), history(6))

	require.Error(t, err)
	assert.Empty(t, store.stored)
}
