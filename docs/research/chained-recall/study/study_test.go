package study

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/agent/memory"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

type constEmbedder struct{}

func (constEmbedder) Embed(context.Context, string) ([]float32, error) { return []float32{1, 0}, nil }

type fixedSummary struct{}

func (fixedSummary) Summarize(context.Context, []string) (string, error) { return "summary", nil }

func TestRedact_MatchesTheProductionStore(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenConversation(filepath.Join(t.TempDir(), "memory.db"), constEmbedder{})
	require.NoError(t, err)
	defer store.Close()
	inputs := []string{
		"my wifi password is hunter2 and the api_key: abc123",
		"use Bearer eyJhbGciOi to call it",
		"the key is sk-abcdefghijklmnop",
		"nothing to hide here",
	}
	for _, in := range inputs {
		require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: in}}, fixedSummary{}))
	}
	for i, in := range inputs {
		stored, err := store.Source(ctx, int64(i+1))
		require.NoError(t, err)
		assert.Equal(t, stored.Content, Redact(in), in)
	}
	assert.NotEqual(t, inputs[0], Redact(inputs[0]), "the control: the first input is one production redacts")
}

func TestRecalled_RendersAMidAsTheProductionRecallDoes(t *testing.T) {
	items := []Item{
		{ID: 7, Level: "mid", Kind: "recent", SourceFirst: 25, SourceLast: 28, Text: " A summary. "},
		{ID: 3, Level: "group", Kind: "historical", SourceFirst: 1, SourceLast: 16, Text: "A group."},
	}
	out := Recalled(items)
	require.Len(t, out, 2)
	assert.Equal(t, agent.RecalledMemory{ID: "7", Content: "A summary. [mid:7 sources:25-28; summary, not verified source text]", Age: "recent"}, out[0])
	assert.Equal(t, "group:3", out[1].ID)
	want := 0
	for _, m := range out {
		want += len(fmt.Sprintf("- [%s] (%s): %s\n", m.ID, m.Age, m.Content))
	}
	assert.Equal(t, want, ContextChars(items))
}

type fakeMemory struct {
	agent.ConversationMemory
	items []Item
}

func (f fakeMemory) Recall(context.Context, string) ([]Item, error) { return f.items, nil }
func (f fakeMemory) Search(_ context.Context, _ string, limit int) ([]Item, error) {
	return f.items[:min(limit, len(f.items))], nil
}
func (f fakeMemory) Store() agent.ConversationMemory { return f.ConversationMemory }
func (f fakeMemory) Close() error                    { return nil }

func TestConversation_AnswersSearchWithTheArmAndLabelsOtherLevels(t *testing.T) {
	c := conversation{memory: fakeMemory{items: []Item{
		{ID: 1, Level: "mid", SourceFirst: 1, SourceLast: 4, Text: "m"},
		{ID: 2, Level: "group", SourceFirst: 1, SourceLast: 16, Text: "g"},
	}}}
	hits, err := c.Search(context.Background(), "q", 5)
	require.NoError(t, err)
	assert.Equal(t, []agent.ConversationHit{{ID: 1, Summary: "m", SourceFirst: 1, SourceLast: 4}, {ID: 2, Summary: "group: g", SourceFirst: 1, SourceLast: 16}}, hits)
	assert.True(t, strings.HasPrefix(hits[1].Summary, "group"))
}

func TestRecaller_AppendsPendingPreviewsAfterTheArmsItems(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenConversation(filepath.Join(t.TempDir(), "memory.db"), constEmbedder{})
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: "not yet a batch"}}, fixedSummary{}))
	r := recaller{memory: fakeMemory{ConversationMemory: store, items: []Item{{ID: 9, Level: "mid", Kind: "historical", SourceFirst: 1, SourceLast: 4, Text: "s"}}}}
	got, err := r.Recall(ctx, "task")
	require.NoError(t, err)
	require.Len(t, got.Memories, 2)
	assert.Equal(t, "9", got.Memories[0].ID)
	assert.Equal(t, agent.RecalledMemory{ID: "source:1", Content: "not yet a batch [source:1; pending user message preview; inspect source for full text]", Age: "pending"}, got.Memories[1])
	empty, err := recaller{memory: fakeMemory{ConversationMemory: store}}.Recall(ctx, "task")
	require.NoError(t, err)
	assert.Len(t, empty.Memories, 1, "the control: with no items only the pending preview remains")
}

// recordingSummary summarizes every batch with its messages joined, so a
// test can search for a batch by its words.
type recordingSummary struct{}

func (recordingSummary) Summarize(_ context.Context, updates []string) (string, error) {
	return strings.Join(updates, " / "), nil
}

// wordEmbedder embeds text by which of a few words it holds.
type wordEmbedder struct{}

func (wordEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	words := []string{"apple", "boat", "cedar", "delta", "ember", "flint"}
	v := make([]float32, len(words)+1)
	v[len(words)] = 0.1
	for i, w := range words {
		if strings.Contains(strings.ToLower(text), w) {
			v[i] = 1
		}
	}
	return v, nil
}

func TestShippedMemory_RecallsWhatProductionRecalls(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenConversation(filepath.Join(t.TempDir(), "memory.db"), wordEmbedder{})
	require.NoError(t, err)
	defer store.Close()
	for i, w := range []string{"apple", "boat", "cedar", "delta", "ember", "flint", "apple boat", "cedar", "delta", "ember"} {
		for j := range 4 {
			require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: fmt.Sprintf("%s note %d-%d", w, i, j)}}, recordingSummary{}))
		}
	}
	for _, task := range []string{"apple", "flint boat", "nothing"} {
		items, err := ShippedMemory{Conversation: store}.Recall(ctx, task)
		require.NoError(t, err)
		production, err := memory.ConversationRecall{Store: store}.Recall(ctx, task)
		require.NoError(t, err)
		require.NotEmpty(t, items, "the control: the store has mids to recall")
		assert.Equal(t, production.Memories, Recalled(items), task)
	}
}
