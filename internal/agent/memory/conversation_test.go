package memory_test

import (
	"context"
	"fmt"
	"hash/fnv"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	agentmemory "github.com/garamsh/sherlock/internal/agent/memory"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

type firstSummary struct{}

func (firstSummary) Summarize(_ context.Context, updates []string) (string, error) {
	return updates[0], nil
}

// wordEmbedder counts each word into a hashed dimension, so texts sharing
// words are similar and texts sharing none are orthogonal.
type wordEmbedder struct{}

func (wordEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	vector := make([]float32, 4096)
	for _, word := range strings.Fields(strings.ToLower(text)) {
		hash := fnv.New32a()
		_, _ = hash.Write([]byte(strings.Trim(word, ".,;:?!")))
		vector[hash.Sum32()%4096]++
	}
	return vector, nil
}

// rankedStore is a conversation store whose recent and similar episodes are
// fixed, recording what Search was asked. Search ignores the limit, so the
// bound under test is Recall's own.
type rankedStore struct {
	agent.ConversationMemory
	recent, similar []agent.ConversationHit
	query           string
	limit           int
}

func (s *rankedStore) RecentMid(_ context.Context, limit int) ([]agent.ConversationHit, error) {
	return s.recent[:min(limit, len(s.recent))], nil
}

func (s *rankedStore) Search(_ context.Context, query string, limit int) ([]agent.ConversationHit, error) {
	s.query, s.limit = query, limit
	return s.similar, nil
}

func (s *rankedStore) PendingSources(context.Context) ([]agent.SourceMessage, error) { return nil, nil }

func mids(ids ...int64) []agent.ConversationHit {
	var out []agent.ConversationHit
	for _, id := range ids {
		out = append(out, agent.ConversationHit{ID: id, Summary: fmt.Sprintf("episode %d", id), SourceFirst: 4*id - 3, SourceLast: 4 * id})
	}
	return out
}

func TestConversationRecall_RecentFirstThenSimilarDeduplicatedAndBounded(t *testing.T) {
	store := &rankedStore{recent: mids(20, 19, 18), similar: mids(19, 3, 11, 7, 20, 5, 9, 2, 14)}
	result, err := (agentmemory.ConversationRecall{Store: store}).Recall(context.Background(), "launch codename")
	require.NoError(t, err)
	assert.Equal(t, "launch codename", store.query, "the task text itself is embedded")
	assert.Equal(t, 7, store.limit)
	var ids, ages []string
	for _, m := range result.Memories {
		ids, ages = append(ids, m.ID), append(ages, m.Age)
		assert.Contains(t, m.Content, "summary, not verified source text")
	}
	assert.Equal(t, []string{"20", "19", "3", "11", "7", "5", "9"}, ids)
	assert.Equal(t, []string{"recent", "recent", "historical", "historical", "historical", "historical", "historical"}, ages)
}

func checkpoint(content string) []agent.SourceMessage {
	var messages []agent.SourceMessage
	for i := 0; i < 4; i++ {
		messages = append(messages, agent.SourceMessage{Role: agent.RoleUser, Content: content})
	}
	return messages
}

func TestConversationRecall_PendingSourcesRemainReachable(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenConversation(filepath.Join(t.TempDir(), "memory.db"), wordEmbedder{})
	require.NoError(t, err)
	defer store.Close()
	for i := 0; i < 3; i++ {
		require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: fmt.Sprintf("Pending fact %d", i)}}, firstSummary{}))
	}
	recent, err := store.Recent(ctx, 2)
	require.NoError(t, err)
	require.Len(t, recent, 2)
	assert.NotContains(t, recent[0].Content, "fact 0")
	recalled, err := (agentmemory.ConversationRecall{Store: store}).Recall(ctx, "unsupported")
	require.NoError(t, err)
	require.Len(t, recalled.Memories, 3)
	assert.Equal(t, "source:1", recalled.Memories[0].ID)
	assert.Contains(t, recalled.Memories[0].Content, "fact 0")
	source, err := store.Source(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, "Pending fact 0", source.Content)
}

func TestConversationRecall_RecentCorrectionAndOlderRelevantEpisode(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenConversation(filepath.Join(t.TempDir(), "memory.db"), wordEmbedder{})
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.RecordTurn(ctx, checkpoint("The current project codename is Blue Cedar."), firstSummary{}))
	for i := 0; i < 9; i++ {
		require.NoError(t, store.RecordTurn(ctx, checkpoint(fmt.Sprintf("Routine project status %d", i)), firstSummary{}))
	}
	require.NoError(t, store.RecordTurn(ctx, checkpoint("Correction: use Silver Pine in place of Blue Cedar. This is final."), firstSummary{}))
	// Relevance alone finds the old assertion and misses the weak-overlap correction.
	hits, err := store.Search(ctx, "current project codename", 5)
	require.NoError(t, err)
	require.Len(t, hits, 5)
	assert.EqualValues(t, 1, hits[0].ID)
	for _, hit := range hits {
		assert.NotEqualValues(t, 11, hit.ID)
	}
	result, err := (agentmemory.ConversationRecall{Store: store}).Recall(ctx, "current project codename")
	require.NoError(t, err)
	require.Len(t, result.Memories, 7)
	assert.Equal(t, "11", result.Memories[0].ID)
	assert.Equal(t, "recent", result.Memories[0].Age)
	var old bool
	for _, m := range result.Memories {
		old = old || m.ID == "1"
	}
	assert.True(t, old, "recent-only selection would lose the older relevant episode")
	assert.Contains(t, result.Memories[0].Content, "Silver Pine")
	assert.Contains(t, result.Memories[0].Content, "sources:41-44")
	source, err := store.Source(ctx, 41)
	require.NoError(t, err)
	assert.Equal(t, "Correction: use Silver Pine in place of Blue Cedar. This is final.", source.Content)
}
