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

// wordEmbedder places a text by which of a few words it holds, with a
// constant component every text shares.
type wordEmbedder struct{}

var words = []string{"horse", "riding", "boat", "water", "bread"}

func (wordEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	v := make([]float32, len(words)+1)
	v[len(words)] = 0.1
	for i, w := range words {
		if strings.Contains(strings.ToLower(text), w) {
			v[i] = 1
		}
	}
	// A riding task and a horse fact meet on one axis.
	if strings.Contains(strings.ToLower(text), "riding") {
		v[0] = 1
	}
	return v, nil
}

type scripted struct{ n int }

func (s *scripted) Summarize(context.Context, []string) (agent.Synthesis, error) {
	s.n++
	return agent.Synthesis{Summary: fmt.Sprintf("the user chatted about the weather, batch %d.", s.n)}, nil
}

// build writes a store of `batches` mids over 4*batches messages, then F's
// facts for them, and opens F's memory over it.
func build(t *testing.T, batches int, facts map[int64][]Fact) *memory {
	t.Helper()
	ctx := context.Background()
	var stream []study.Message
	for i := range 4 * batches {
		stream = append(stream, study.Message{ID: int64(i + 1), Role: "user", Content: fmt.Sprintf("message %d", i+1)})
	}
	env := &study.Env{Corpus: &study.Corpus{Stream: stream}, Embedder: wordEmbedder{}, Work: t.TempDir(), Arm: "F"}
	dir := filepath.Join(env.Work, "store")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	store, err := sqlite.OpenConversation(filepath.Join(dir, "memory.db"), env.Embedder)
	require.NoError(t, err)
	s := &scripted{}
	for _, m := range stream {
		require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: m.Content}}, s))
	}
	require.NoError(t, store.Close())
	written, _, _, err := writeFacts(ctx, env, filepath.Join(dir, "memory.db"), facts)
	require.NoError(t, err)
	n := 0
	for _, fs := range facts {
		n += len(fs)
	}
	require.Equal(t, n, written, "the setup: every fact written")
	opened, err := arm{}.Open(ctx, env, "b4", dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = opened.Close() })
	return opened.(*memory)
}

func kinds(items []study.Item, kind string) []study.Item {
	var out []study.Item
	for _, it := range items {
		if it.Kind == kind {
			out = append(out, it)
		}
	}
	return out
}

func TestRecall_AddsTheProfileAndSimilarFactsAfterTheShippedRecall(t *testing.T) {
	m := build(t, 10, map[int64][]Fact{
		1:  {{Kind: "user", Text: "The user's horse is called Wouvull."}},
		21: {{Kind: "other", Text: "The bakery on the corner sells rye bread."}},
		37: {{Kind: "user", Text: "The user sails a boat named Quelra."}},
	})
	items, err := m.Recall(context.Background(), "Write a caption for my photo out riding.")
	require.NoError(t, err)
	shipped, err := m.ShippedMemory.Recall(context.Background(), "Write a caption for my photo out riding.")
	require.NoError(t, err)
	assert.Equal(t, shipped, items[:len(shipped)], "the shipped recall comes first, unchanged")
	profile := kinds(items, "profile")
	require.Len(t, profile, 2)
	assert.Equal(t, "The user sails a boat named Quelra.", profile[0].Text, "newest mid first")
	assert.Equal(t, int64(1), profile[1].SourceFirst, "a fact carries its mid's interval")
	similar := kinds(items, "fact")
	require.Len(t, similar, 1, "facts already in the profile are not delivered twice")
	assert.Equal(t, "The bakery on the corner sells rye bread.", similar[0].Text)
}

func TestProfile_KeepsOnlyUserFactsWithinItsBound(t *testing.T) {
	long := strings.Repeat("x", 199)
	facts := map[int64][]Fact{}
	for i := range 8 {
		facts[int64(4*i+1)] = []Fact{{Kind: "user", Text: fmt.Sprintf("%d%s", i, long)}, {Kind: "other", Text: "other fact"}}
	}
	m := build(t, 8, facts)
	profile := m.profile()
	chars := 0
	for _, it := range profile {
		assert.NotEqual(t, "other fact", it.Text)
		chars += len([]rune(it.Text))
	}
	assert.Len(t, profile, 5, "five 200-character facts fill the 1,000-character block")
	assert.LessOrEqual(t, chars, profileChars)
	assert.Equal(t, "7"+long, profile[0].Text, "the newest first")
}

func TestSimilar_ReturnsTheClosestFactsUpToTheBound(t *testing.T) {
	facts := map[int64][]Fact{
		1:  {{Kind: "other", Text: "a horse fact"}, {Kind: "other", Text: "another horse fact"}},
		5:  {{Kind: "other", Text: "a horse and boat fact"}, {Kind: "other", Text: "a third horse fact"}},
		9:  {{Kind: "other", Text: "a bread fact"}},
		13: {{Kind: "other", Text: "a water fact"}},
	}
	m := build(t, 4, facts)
	similar, err := m.similar(context.Background(), "riding", map[int64]bool{})
	require.NoError(t, err)
	require.Len(t, similar, similarFacts)
	for _, it := range similar {
		assert.Contains(t, it.Text, "horse")
	}
	// Control: a query nearest to other facts gets them.
	similar, err = m.similar(context.Background(), "bread", map[int64]bool{})
	require.NoError(t, err)
	assert.Equal(t, "a bread fact", similar[0].Text)
}
