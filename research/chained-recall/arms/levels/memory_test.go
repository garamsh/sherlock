package main

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent/memory"
	"github.com/garamsh/sherlock/research/chained-recall/study"
)

// openBuilt builds H at g4 over n mids and opens it.
func openBuilt(t *testing.T, n int) *levels {
	env, _ := newEnv(t, n)
	dir := filepath.Join(env.Work, "builds", "H", "g4")
	_, err := arm{}.Build(context.Background(), env, "g4", dir)
	require.NoError(t, err)
	opened, err := arm{}.Open(context.Background(), env, "g4", dir)
	require.NoError(t, err)
	t.Cleanup(func() { opened.Close() })
	return opened.(*levels)
}

func TestSearch_RanksGroupsAndMidsInOneList(t *testing.T) {
	l := openBuilt(t, 12)
	ctx := context.Background()

	// Words of two mids in one window: only their group holds both.
	hits, err := l.Search(ctx, "alpha5 alpha7", 1)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, study.Item{ID: 2, Level: "group", Kind: "historical", SourceFirst: 17, SourceLast: 32, Text: hits[0].Text}, hits[0])
	assert.Equal(t, map[string]any{"groups": 1, "mids": 0}, l.LastTrace())

	// The control: one mid's own words rank that mid above its group.
	hits, err = l.Search(ctx, "alpha5 beta5", 1)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, study.Item{ID: 5, Level: "mid", Kind: "historical", SourceFirst: 17, SourceLast: 20, Text: midText(5)}, hits[0])
	assert.Equal(t, map[string]any{"groups": 0, "mids": 1}, l.LastTrace())
}

func TestRecall_DeliversGroupsWithinTheCharactersOfTheShippedRecall(t *testing.T) {
	l := openBuilt(t, 12)
	ctx := context.Background()
	task := "alpha1 alpha3"

	shipped, err := memory.ConversationRecall{Store: l.Store()}.Recall(ctx, task)
	require.NoError(t, err)
	require.Len(t, shipped.Memories, 7, "S delivers the two newest and five similar mids; nothing is pending")
	budget := 0
	for _, m := range shipped.Memories {
		budget += len([]rune(fmt.Sprintf("- [%s] (%s): %s\n", m.ID, m.Age, m.Content)))
	}

	items, err := l.Recall(ctx, task)
	require.NoError(t, err)
	trace := l.LastTrace()
	assert.Equal(t, budget, trace["budget"], "the budget is what the shipped recall delivers for the task")
	assert.LessOrEqual(t, study.ContextChars(items), budget)

	require.GreaterOrEqual(t, len(items), 3)
	assert.Equal(t, []int64{12, 11}, []int64{items[0].ID, items[1].ID}, "the two newest mids come first")
	assert.Equal(t, "recent", items[0].Kind)
	assert.Equal(t, study.Item{ID: 1, Level: "group", Kind: "historical", SourceFirst: 1, SourceLast: 16, Text: items[2].Text}, items[2], "the group holding both words leads the ranked part")
	seen, groups := map[string]bool{}, 0
	for _, it := range items {
		key := fmt.Sprintf("%s:%d", it.Level, it.ID)
		assert.False(t, seen[key], "%s delivered twice", key)
		seen[key] = true
		if it.Level == "group" {
			groups++
		}
	}
	assert.Equal(t, groups, trace["groups"], "the trace counts the groups delivered")
}
