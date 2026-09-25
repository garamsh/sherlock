package memory_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
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

func TestConversationRecall_UnusablePhrasesFallBackButMissesDoNot(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenConversation(filepath.Join(t.TempDir(), "memory.db"))
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.RecordTurn(ctx, checkpoint("The launch codename is cobalt"), firstSummary{}))
	for i := 0; i < 9; i++ {
		require.NoError(t, store.RecordTurn(ctx, checkpoint(fmt.Sprintf("Routine event %d", i)), firstSummary{}))
	}
	tests := []struct {
		name, phrases string
		wantOld       bool
	}{
		{"empty list", `[]`, true}, {"empty", `[""]`, true}, {"whitespace", `["   "]`, true}, {"punctuation", `["!!!"]`, true}, {"stop words", `["the","and"]`, true},
		{"usable miss", `["violet submarine"]`, false}, {"mixed", `["!","cobalt"]`, true}, {"bounded phrases", `["!","!","!","!","violet"]`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := agent.NewScriptedModel(agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: "memory_search_terms", Input: json.RawMessage(`{"phrases":` + tt.phrases + `}`)}}})
			result, err := (agentmemory.ConversationRecall{Store: store, Model: model}).Recall(ctx, "launch codename cobalt")
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(result.Memories), 2)
			assert.Equal(t, "10", result.Memories[0].ID)
			assert.Equal(t, "recent", result.Memories[0].Age)
			found := false
			for _, m := range result.Memories {
				found = found || m.ID == "1"
			}
			assert.Equal(t, tt.wantOld, found)
		})
	}
	result, err := (agentmemory.ConversationRecall{Store: store}).Recall(ctx, "routine event")
	require.NoError(t, err)
	require.Len(t, result.Memories, 7)
	seen := map[string]bool{}
	for _, m := range result.Memories {
		assert.False(t, seen[m.ID])
		seen[m.ID] = true
		assert.Contains(t, m.Content, "summary, not verified source text")

	}
}

func checkpoint(content string) []agent.SourceMessage {
	var messages []agent.SourceMessage
	for i := 0; i < 8; i++ {
		messages = append(messages, agent.SourceMessage{Role: agent.RoleUser, Content: content})
	}
	return messages
}

func TestConversationRecall_PendingSourcesRemainReachable(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenConversation(filepath.Join(t.TempDir(), "memory.db"))
	require.NoError(t, err)
	defer store.Close()
	for i := 0; i < 7; i++ {
		require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: fmt.Sprintf("Pending fact %d", i)}}, firstSummary{}))
	}
	recent, err := store.Recent(ctx, 2)
	require.NoError(t, err)
	require.Len(t, recent, 2)
	assert.NotContains(t, recent[0].Content, "fact 0")
	recalled, err := (agentmemory.ConversationRecall{Store: store}).Recall(ctx, "unsupported")
	require.NoError(t, err)
	require.Len(t, recalled.Memories, 7)
	assert.Equal(t, "source:1", recalled.Memories[0].ID)
	assert.Contains(t, recalled.Memories[0].Content, "fact 0")
	source, err := store.Source(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, "Pending fact 0", source.Content)
}

func TestConversationRecall_RecentCorrectionAndOlderRelevantEpisode(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenConversation(filepath.Join(t.TempDir(), "memory.db"))
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.RecordTurn(ctx, checkpoint("The current project codename is Blue Cedar."), firstSummary{}))
	for i := 0; i < 9; i++ {
		require.NoError(t, store.RecordTurn(ctx, checkpoint(fmt.Sprintf("Routine project status %d", i)), firstSummary{}))
	}
	require.NoError(t, store.RecordTurn(ctx, checkpoint("Correction: use Silver Pine in place of Blue Cedar. This is final."), firstSummary{}))
	// Relevance alone finds the old assertion and misses the weak-overlap correction.
	hits, err := store.Search(ctx, []string{"current project codename"}, 5)
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
	assert.Contains(t, result.Memories[0].Content, "sources:81-88")
	source, err := store.Source(ctx, 81)
	require.NoError(t, err)
	assert.Equal(t, "Correction: use Silver Pine in place of Blue Cedar. This is final.", source.Content)
	// No overlap supplies only recent context, never a fabricated relevance match.
	hits, err = store.Search(ctx, []string{"orbital submarine"}, 5)
	require.NoError(t, err)
	assert.Empty(t, hits)
	result, err = (agentmemory.ConversationRecall{Store: store}).Recall(ctx, "orbital submarine")
	require.NoError(t, err)
	require.Len(t, result.Memories, 2)
	for _, m := range result.Memories {
		assert.Equal(t, "recent", m.Age)
	}
}
