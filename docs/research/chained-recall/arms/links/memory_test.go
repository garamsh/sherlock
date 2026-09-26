package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/docs/research/chained-recall/study"
	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

// markerEmbedder places a text by which of the chain's words it holds, with
// a constant component every text shares. A query naming only the chain's
// start is then closer to any unrelated mid than to hops 2 and 3, as the
// corpus's chain check requires of the real embedder.
type markerEmbedder struct{}

func (markerEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	v := []float32{0, 0, 0, 0.3}
	for i, w := range []string{"rodend", "gebind", "mateo"} {
		if strings.Contains(strings.ToLower(text), w) {
			v[i] = 1
		}
	}
	return v, nil
}

// scripted answers each synthesis request with the next summary.
type scripted struct{ summaries []string }

func (s *scripted) Summarize(context.Context, []string) (string, error) {
	out := s.summaries[0]
	s.summaries = s.summaries[1:]
	return out, nil
}

var chainSummaries = []string{
	"Every part for the Rodend build is ordered from Gebind Components.",
	"Our account at Gebind Components is looked after by Mateo Deatreal.",
	"Mateo Deatreal answers on extension 5504.",
}

const chainQuery = "Something is wrong with an order for the Rodend build. What number do I dial?"

// sharedStore writes a shared batch-4 store holding the chain's three mids
// first, then twelve unrelated ones, and returns the study env over it.
func sharedStore(t *testing.T) *study.Env {
	t.Helper()
	ctx := context.Background()
	env := &study.Env{Embedder: markerEmbedder{}, Work: t.TempDir(), Arm: "L"}
	dir := filepath.Join(env.Work, "shared", "b4")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	store, err := sqlite.OpenConversation(filepath.Join(dir, "memory.db"), env.Embedder)
	require.NoError(t, err)
	summaries := append([]string(nil), chainSummaries...)
	for i := 1; i <= 12; i++ {
		summaries = append(summaries, fmt.Sprintf("the user talked about watering the garden, day %d.", i))
	}
	summarizer := &scripted{summaries: summaries}
	for i := range 4 * len(summaries) {
		require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: fmt.Sprintf("message %d", i+1)}}, summarizer))
	}
	require.NoError(t, store.Close())
	mids, _, err := study.MidStats(filepath.Join(dir, "memory.db"))
	require.NoError(t, err)
	require.Equal(t, 15, mids, "the setup: one mid per batch of four")
	return env
}

func open(t *testing.T, env *study.Env) *memory {
	t.Helper()
	ctx := context.Background()
	dir := filepath.Join(env.Work, "builds", "L", "b4")
	built, err := New().Build(ctx, env, "b4", dir)
	require.NoError(t, err)
	require.Equal(t, 3, built.Notes["mids_with_entities"], "the setup: only the chain's mids name entities")
	m, err := New().Open(ctx, env, "b4", dir)
	require.NoError(t, err)
	t.Cleanup(func() { m.Close() })
	return m.(*memory)
}

func itemIDs(items []study.Item) []int64 {
	out := []int64{}
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

// shipped is the shipped recall over the same store: two recent mids and
// five similar ones.
func shipped(t *testing.T, m *memory, task string) []int64 {
	t.Helper()
	ctx := context.Background()
	recent, err := m.store.RecentMid(ctx, recentMids)
	require.NoError(t, err)
	similar, err := m.store.Search(ctx, task, historicalSlots+len(recent))
	require.NoError(t, err)
	return itemIDs(take(recentMids+historicalSlots, map[int64]bool{}, &list{"", append(recent, similar...)}))
}

func TestRecall_DeliversEveryHopWithinTheShippedBudget(t *testing.T) {
	m := open(t, sharedStore(t))
	ctx := context.Background()
	s := shipped(t, m, chainQuery)
	assert.Equal(t, []int64{15, 14, 1, 13, 12, 11, 10}, s, "the control: similarity alone reaches hop 1 only")
	items, err := m.Recall(ctx, chainQuery)
	require.NoError(t, err)
	assert.Equal(t, []int64{15, 14, 1, 2, 13, 3, 12}, itemIDs(items))
	assert.Len(t, items, len(s), "the walk takes similar mids' places and adds none")
	kinds := map[int64]string{}
	for _, it := range items {
		kinds[it.ID] = it.Kind
	}
	assert.Equal(t, "linked", kinds[2])
	assert.Equal(t, "linked", kinds[3])
	assert.Equal(t, int64(9), items[5].SourceFirst, "an item keeps its mid's source interval")
	trace := m.LastTrace()
	assert.Equal(t, []string{"rodend"}, trace["seeds"])
	assert.Greater(t, trace["steps"], 1)
	assert.Equal(t, []int64{2, 3}, trace["beyond_similar"])
	assert.Equal(t, []int64{2, 3}, trace["beyond_seeds"])
}

func TestRecall_IsTheShippedRecallWhenTheQueryNamesNoEntity(t *testing.T) {
	m := open(t, sharedStore(t))
	query := "What is the episode number?"
	items, err := m.Recall(context.Background(), query)
	require.NoError(t, err)
	assert.Equal(t, shipped(t, m, query), itemIDs(items))
	assert.Empty(t, m.LastTrace()["linked"])
}

func TestSearch_PutsAHopsOwnMidFirstForItsStatement(t *testing.T) {
	m := open(t, sharedStore(t))
	items, err := m.Search(context.Background(), "Mateo Deatreal answers on extension 5504.", study.OracleTop)
	require.NoError(t, err)
	require.Len(t, items, study.OracleTop)
	assert.Equal(t, int64(3), items[0].ID)
	assert.Contains(t, itemIDs(items), int64(2))
}

func TestBuild_LeavesTheSharedStoreUnchanged(t *testing.T) {
	env := sharedStore(t)
	path := filepath.Join(env.Work, "shared", "b4", "memory.db")
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	open(t, env)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after)
	_, err = readEntities(context.Background(), path)
	assert.Error(t, err, "the shared store holds no entity table")
}
