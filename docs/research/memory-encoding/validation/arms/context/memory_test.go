package main

import (
	"context"
	"encoding/json"
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

// cutEmbedder places a text by which of a few words its first cut
// characters hold, as an embedder with a bounded window reads only the
// start of a long text. Every text shares a constant component.
type cutEmbedder struct{}

var words = []string{"goudre", "studio", "harbour", "bread"}

const cut = 60

func (cutEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	head := strings.ToLower(text)
	if len(head) > cut {
		head = head[:cut]
	}
	v := make([]float32, len(words)+1)
	v[len(words)] = 0.1
	for i, w := range words {
		if strings.Contains(head, w) {
			v[i] = 1
		}
	}
	return v, nil
}

type scripted struct{ n int }

func (s *scripted) Summarize(context.Context, []string) (string, error) {
	s.n++
	return fmt.Sprintf("recorded summary %d about bread.", s.n), nil
}

// longSummary outruns the embedder's cut, so only what comes first is read.
var longSummary = "The user gave an extension, 4864, as the best line." + strings.Repeat(" More words.", 10)

// built writes a shared store of two base and four stream batches with
// recorded summaries, records C's synthesis of the stream, and runs C's
// Build over it.
func built(t *testing.T) (*study.Env, string, study.Built) {
	t.Helper()
	ctx := context.Background()
	env := &study.Env{Corpus: &study.Corpus{Base: numbered(1, 8), Stream: numbered(9, 16)}, Embedder: cutEmbedder{}, Dir: t.TempDir(), Work: t.TempDir(), Arm: "C"}
	shared := filepath.Join(env.Work, "shared", "b4")
	require.NoError(t, os.MkdirAll(shared, 0o755))
	store, err := sqlite.OpenConversation(filepath.Join(shared, "memory.db"), env.Embedder)
	require.NoError(t, err)
	s := &scripted{}
	for _, m := range env.Corpus.Messages() {
		require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: m.Content}}, s))
	}
	require.NoError(t, store.Close())
	require.Equal(t, 6, s.n, "the setup: six recorded mids")

	var lines []string
	for _, b := range []batchContext{
		{First: 9, Last: 12, Summary: longSummary, Context: "A call about reaching Oscar Goudre."},
		{First: 13, Last: 16, Summary: "The user moved the easel.", Context: "At the studio."},
		{First: 17, Last: 20, Summary: "C's summary without context.", Context: ""},
		{First: 21, Last: 24, Failed: "no answer after two builds"},
	} {
		line, _ := json.Marshal(b)
		lines = append(lines, string(line))
	}
	path := contextsPath(env)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644))

	dir := filepath.Join(env.Work, "build")
	b, err := arm{}.Build(ctx, env, setting, dir)
	require.NoError(t, err)
	return env, dir, b
}

func open(t *testing.T, env *study.Env, dir string) study.Memory {
	t.Helper()
	m, err := arm{}.Open(context.Background(), env, setting, dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func TestBuild_StoresCsSummaryAndContextForStreamMidsOnly(t *testing.T) {
	env, dir, b := built(t)
	m := open(t, env, dir)
	items, err := m.Search(context.Background(), "anything", 10)
	require.NoError(t, err)
	byFirst := map[int64]string{}
	for _, it := range items {
		byFirst[it.SourceFirst] = it.Text
	}
	require.Len(t, byFirst, 6)
	assert.Equal(t, "recorded summary 1 about bread.", byFirst[1], "a base mid keeps its recorded summary and has no context")
	assert.Equal(t, "recorded summary 2 about bread.", byFirst[5])
	assert.Equal(t, strings.TrimSpace(longSummary)+"\n"+contextLabel+"A call about reaching Oscar Goudre.", byFirst[9])
	assert.Equal(t, "C's summary without context.", byFirst[17], "an empty context adds no line")
	assert.Equal(t, "recorded summary 6 about bread.", byFirst[21], "a batch recorded as failed keeps the shared mid")

	contextChars := len("A call about reaching Oscar Goudre.") + len("At the studio.")
	summaryChars := 3*len("recorded summary 1 about bread.") + len(strings.TrimSpace(longSummary)) + len("The user moved the easel.") + len("C's summary without context.")
	assert.Equal(t, study.Built{
		StoredChars: summaryChars + contextChars, Vectors: 6,
		Units: map[string]int{"mid": 6, "context": 2},
		Notes: map[string]any{"context_chars": contextChars, "mids_with_context": 2},
	}, b)
}

func TestBuild_EmbedsTheContextBeforeTheSummary(t *testing.T) {
	env, dir, _ := built(t)
	m := open(t, env, dir)
	// Goudre is named only in the context of a mid whose summary is longer
	// than the embedder reads.
	items, err := m.Search(context.Background(), "Oscar Goudre", 1)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, int64(9), items[0].SourceFirst, "the context is read before the summary is cut")
	// Control: a word only in a summary still finds its mid.
	items, err = m.Search(context.Background(), "bread", 5)
	require.NoError(t, err)
	assert.Equal(t, int64(21), items[0].SourceFirst, "the newest of the mids that name it")
}

func TestRecall_ShowsEachMidsContextLabelledAfterItsSummary(t *testing.T) {
	env, dir, _ := built(t)
	m := open(t, env, dir)
	items, err := m.Recall(context.Background(), "the studio")
	require.NoError(t, err)
	shipped := study.ShippedMemory{Conversation: m.(memory).shipped.Conversation}
	plain, err := shipped.Recall(context.Background(), "the studio")
	require.NoError(t, err)
	require.Len(t, items, len(plain), "the shipped recall's items, in its order")
	labelled := 0
	for i, it := range items {
		assert.Equal(t, plain[i].ID, it.ID)
		if c := m.(memory).contexts[it.ID]; c != "" {
			assert.Equal(t, strings.TrimSpace(plain[i].Text)+"\n"+contextLabel+c, it.Text)
			labelled++
		} else {
			assert.Equal(t, plain[i].Text, it.Text)
		}
	}
	assert.Equal(t, 2, labelled, "both mids with a context are delivered and labelled")
}

func TestBuild_KeyOnlyShowsSsSummaryAndRanksByTheContext(t *testing.T) {
	env, _, _ := built(t)
	ctx := context.Background()
	dir := filepath.Join(env.Work, "build-key")
	b, err := arm{}.Build(ctx, env, keyOnly, dir)
	require.NoError(t, err)
	m, err := arm{}.Open(ctx, env, keyOnly, dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })
	items, err := m.Search(ctx, "anything", 10)
	require.NoError(t, err)
	require.Len(t, items, 6)
	for _, it := range items {
		n := (it.SourceFirst-1)/4 + 1
		assert.Equal(t, fmt.Sprintf("recorded summary %d about bread.", n), it.Text, "every mid shows its shared summary, with no context line")
	}
	top, err := m.Search(ctx, "Oscar Goudre", 1)
	require.NoError(t, err)
	assert.Equal(t, int64(9), top[0].SourceFirst, "ranked by the context in its key")
	assert.NotContains(t, top[0].Text, "Goudre", "which the shown text does not hold")
	// Control: C's own build ranks the same mid first, so the key, not the
	// summary C wrote, carries the rank.
	c := open(t, env, filepath.Join(env.Work, "build"))
	cTop, err := c.Search(ctx, "Oscar Goudre", 1)
	require.NoError(t, err)
	assert.Equal(t, int64(9), cTop[0].SourceFirst)
	assert.Contains(t, cTop[0].Text, contextLabel, "C shows the context; C-key does not")
	contextChars := len("A call about reaching Oscar Goudre.") + len("At the studio.")
	assert.Equal(t, study.Built{
		StoredChars: 6*len("recorded summary 1 about bread.") + contextChars, Vectors: 6,
		Units: map[string]int{"mid": 6, "context": 2},
		Notes: map[string]any{"context_chars": contextChars, "mids_with_context": 2},
	}, b)
}
