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

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
	"github.com/garamsh/sherlock/research/chained-recall/study"
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
	env := &study.Env{Embedder: markerEmbedder{}, Work: t.TempDir(), Dir: t.TempDir(), Arm: "P"}
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

// openLinked copies the shared store, writes the given links into the copy
// and opens it as P does.
func openLinked(t *testing.T, env *study.Env, links map[int64][]int64) *memory {
	t.Helper()
	ctx := context.Background()
	shared, err := env.SharedStore(4)
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, study.CopyDir(shared, dir))
	written, err := writeLinks(ctx, filepath.Join(dir, "memory.db"), links)
	require.NoError(t, err)
	want := 0
	for _, l := range links {
		want += len(l)
	}
	require.Equal(t, want, written, "the setup: every link is written")
	m, err := New().Open(ctx, env, "b4-k5", dir)
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

func kinds(items []study.Item) map[int64]string {
	out := map[int64]string{}
	for _, it := range items {
		out[it.ID] = it.Kind
	}
	return out
}

// shipped is arm S's recall over the same store.
func shipped(t *testing.T, m *memory, task string) []int64 {
	t.Helper()
	items, err := study.ShippedMemory{Conversation: m.store}.Recall(context.Background(), task)
	require.NoError(t, err)
	return itemIDs(items)
}

var chainLinks = map[int64][]int64{2: {1}, 3: {2}}

func TestRecall_DeliversEveryLinkedHopWithinTheShippedBudget(t *testing.T) {
	env := sharedStore(t)
	ctx := context.Background()
	unlinked := openLinked(t, env, nil)
	s := shipped(t, unlinked, chainQuery)
	assert.Equal(t, []int64{15, 14, 1, 13, 12, 11, 10}, s, "the control: similarity alone reaches hop 1 only")
	plain, err := unlinked.Recall(ctx, chainQuery)
	require.NoError(t, err)
	assert.Equal(t, s, itemIDs(plain), "the control: without links P recalls what S recalls")

	m := openLinked(t, env, chainLinks)
	items, err := m.Recall(ctx, chainQuery)
	require.NoError(t, err)
	assert.Equal(t, []int64{15, 14, 1, 2, 13, 3, 12}, itemIDs(items))
	assert.Len(t, items, len(s), "linked mids take similar mids' places and add none")
	k := kinds(items)
	assert.Equal(t, "recent", k[15])
	assert.Equal(t, "historical", k[1])
	assert.Equal(t, "thread", k[2])
	assert.Equal(t, "thread", k[3])
	assert.Equal(t, map[string]any{"thread": []int64{2, 3}, "beyond_similar": []int64{2, 3}}, m.LastTrace())
}

func TestRecall_FollowsALinkFromTheLaterMidToTheEarlier(t *testing.T) {
	env := sharedStore(t)
	// Mid 1 is delivered by similarity and was continued by mid 2: the link
	// is stored on mid 2 and followed from mid 1.
	m := openLinked(t, env, map[int64][]int64{2: {1}})
	items, err := m.Recall(context.Background(), chainQuery)
	require.NoError(t, err)
	assert.Equal(t, "thread", kinds(items)[2])
	assert.NotContains(t, itemIDs(items), int64(3), "the control: an unlinked hop stays out")
}

func TestRecall_TakesTheLinkedNeighbourTheRankingPutsFirst(t *testing.T) {
	env := sharedStore(t)
	// Mid 13, delivered by similarity, continues mids 2 and 3. The Rodend
	// query ranks mid 3 above mid 2, so mid 3 comes first although its ID
	// is the larger.
	m := openLinked(t, env, map[int64][]int64{13: {2, 3}})
	items, err := m.Recall(context.Background(), chainQuery)
	require.NoError(t, err)
	assert.Equal(t, []int64{15, 14, 1, 13, 3, 12, 2}, itemIDs(items))
	assert.Equal(t, "thread", kinds(items)[3])
	assert.Equal(t, "thread", kinds(items)[2])
}

func TestRecall_DoesNotFollowTheRecentMidsLinks(t *testing.T) {
	env := sharedStore(t)
	// Mid 3 ranks last for the query. Linked from the recent mid 15 it stays
	// out; linked from mid 1, which similarity delivers, it comes in.
	m := openLinked(t, env, map[int64][]int64{15: {3}})
	items, err := m.Recall(context.Background(), chainQuery)
	require.NoError(t, err)
	assert.NotContains(t, itemIDs(items), int64(3))
	control := openLinked(t, env, map[int64][]int64{3: {1}})
	items, err = control.Recall(context.Background(), chainQuery)
	require.NoError(t, err)
	assert.Equal(t, "thread", kinds(items)[3], "the control: the same mid linked from a historical mid is delivered")
}

func TestSearch_AlternatesSimilarAndLinkedWithinTheLimit(t *testing.T) {
	env := sharedStore(t)
	m := openLinked(t, env, chainLinks)
	items, err := m.Search(context.Background(), chainQuery, 3)
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 2, 15}, itemIDs(items))
	assert.Equal(t, "thread", kinds(items)[2])
	unlinked := openLinked(t, env, nil)
	plain, err := unlinked.Search(context.Background(), chainQuery, 3)
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 15, 14}, itemIDs(plain), "the control: without links it is the shipped search")
}
