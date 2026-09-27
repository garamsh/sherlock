package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
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

// shipped is arm S's recall over the same store.
func shipped(t *testing.T, m *memory, task string) []int64 {
	t.Helper()
	items, err := study.ShippedMemory{Conversation: m.store}.Recall(context.Background(), task)
	require.NoError(t, err)
	return itemIDs(items)
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

func TestRecall_LabelsWalkDeliveredMidsAsAssociatedInLPlusA(t *testing.T) {
	env := sharedStore(t)
	ctx := context.Background()
	dir := filepath.Join(env.Work, "builds", "L", "b4-model-a")
	require.NoError(t, study.CopyDir(filepath.Join(env.Work, "shared", "b4"), dir))
	path := filepath.Join(dir, "memory.db")
	require.NoError(t, writeEntities(ctx, path, map[int64][]string{
		// Mid 2 names the farther entity first, so only the walk score picks
		// the one it was reached through.
		1: {"rodend", "gebind components"}, 2: {"mateo deatreal", "gebind components"}, 3: {"mateo deatreal"},
	}))
	opened, err := New().Open(ctx, env, "b4-model-a", dir)
	require.NoError(t, err)
	defer opened.Close()
	m := opened.(*memory)
	items, err := m.Recall(ctx, chainQuery)
	require.NoError(t, err)
	byID := map[int64]study.Item{}
	for _, it := range items {
		byID[it.ID] = it
	}
	assert.Equal(t, "associated", byID[2].Kind)
	assert.Equal(t, associatedLabel("gebind components")+chainSummaries[1], byID[2].Text)
	assert.Equal(t, associatedLabel("mateo deatreal")+chainSummaries[2], byID[3].Text)
	assert.Equal(t, "historical", byID[1].Kind, "the control: a similar mid carries no label")
	assert.Equal(t, chainSummaries[0], byID[1].Text)
	assert.Equal(t, []int64{2, 3}, m.LastTrace()["linked"], "the trace still counts the walk's mids")
	hits, err := m.Search(ctx, chainQuery, 5)
	require.NoError(t, err)
	labelled := 0
	for _, it := range hits {
		if strings.HasPrefix(it.Text, "Associated through") {
			labelled++
		}
	}
	assert.Equal(t, 2, labelled, "memory_search labels the walk's mids too")
	assert.Equal(t, Abstention, New().(study.SettingInstructor).InstructionsFor("b4-model-a"))
	assert.Empty(t, New().(study.SettingInstructor).InstructionsFor("b4-model"), "the control: L has no instruction")
}

// openModel opens a setting over the shared store with the chain's
// entities written as the synthesis call would write them.
func openModel(t *testing.T, env *study.Env, setting string) study.Memory {
	t.Helper()
	ctx := context.Background()
	dir := filepath.Join(env.Work, "builds", "L", setting)
	require.NoError(t, study.CopyDir(filepath.Join(env.Work, "shared", "b4"), dir))
	require.NoError(t, writeEntities(ctx, filepath.Join(dir, "memory.db"), map[int64][]string{
		1: {"rodend", "gebind components"}, 2: {"gebind components", "mateo deatreal"}, 3: {"mateo deatreal"},
	}))
	m, err := New().Open(ctx, env, setting, dir)
	require.NoError(t, err)
	t.Cleanup(func() { m.Close() })
	return m
}

func TestRecall_GateDropsWalkMidsNamingNoQueryEntityInLPlusG(t *testing.T) {
	env := sharedStore(t)
	ctx := context.Background()
	ungated, err := openModel(t, env, "b4-model").Recall(ctx, chainQuery)
	require.NoError(t, err)
	assert.Contains(t, itemIDs(ungated), int64(2), "the control: L delivers the walk's mids beyond the seed")
	g := openModel(t, env, "b4-model-g").(*memory)
	items, err := g.Recall(ctx, chainQuery)
	require.NoError(t, err)
	assert.NotContains(t, itemIDs(items), int64(2))
	assert.NotContains(t, itemIDs(items), int64(3))
	assert.Len(t, items, len(ungated), "a dropped walk mid gives its slot back to similarity")
	gate := g.LastTrace()["gate"].([]map[string]any)
	dropped := map[int64]bool{}
	for _, w := range gate {
		dropped[w["id"].(int64)] = w["dropped"].(bool)
		assert.Contains(t, w, "score")
	}
	assert.Equal(t, map[int64]bool{2: true, 3: true}, dropped, "the trace names what ungated L delivers and what the gate dropped")
	assert.NotContains(t, openModel(t, env, "b4-model").(*memory).LastTrace(), "gate", "the control: L records no gate")
}

func TestGate_PassesAMidNamingASeedOrScoringAtTheThreshold(t *testing.T) {
	g := openModel(t, sharedStore(t), "b4-model-g").(*memory)
	linked := []agent.ConversationHit{{ID: 1}, {ID: 2}, {ID: 3}}
	below := math.Nextafter(gateScore, 0)
	kept := g.gate(linked, []string{"rodend"}, map[int64]float64{1: 0, 2: below, 3: gateScore})
	assert.Equal(t, []int64{1, 3}, []int64{kept[0].ID, kept[1].ID}, "mid 1 names the seed; mid 3 reaches the threshold")
	assert.Len(t, kept, 2, "mid 2 names no seed and scores below the threshold")
}

func TestLT_RecallsAsShippedAndWalksOnlyOnCall(t *testing.T) {
	env := sharedStore(t)
	ctx := context.Background()
	m := openModel(t, env, "b4-model-t")
	a, ok := m.(study.Associator)
	require.True(t, ok, "L-T answers memory_associate")
	_, ok = openModel(t, env, "b4-model").(study.Associator)
	assert.False(t, ok, "the control: L does not")
	items, err := m.Recall(ctx, chainQuery)
	require.NoError(t, err)
	store := m.Store().(*sqlite.Store)
	shippedItems, err := study.ShippedMemory{Conversation: store}.Recall(ctx, chainQuery)
	require.NoError(t, err)
	assert.Equal(t, shippedItems, items, "pre-turn recall is S's")
	hits, err := m.Search(ctx, chainQuery, 5)
	require.NoError(t, err)
	for _, it := range hits {
		assert.Equal(t, "historical", it.Kind, "memory_search is S's")
	}
	walked, err := a.Associate(ctx, "Rodend", 5)
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 2, 3}, itemIDs(walked))
	for _, it := range walked {
		assert.Equal(t, "linked", it.Kind)
	}
	assert.Len(t, must(a.Associate(ctx, "Rodend", 2)), 2, "the call's limit bounds it")
	assert.Empty(t, must(a.Associate(ctx, "the garden", 5)), "the control: a query naming no stored entity reaches nothing")
}

func must(items []study.Item, err error) []study.Item {
	if err != nil {
		panic(err)
	}
	return items
}

func TestInstructionsFor_GivesEachVariantItsOwnSentence(t *testing.T) {
	si := New().(study.SettingInstructor)
	assert.Equal(t, OnCall, si.InstructionsFor("b4-model-t"))
	assert.Equal(t, Verify, si.InstructionsFor("b4-model-v"))
	assert.Empty(t, si.InstructionsFor("b4-model-g"), "L+G changes only what recall delivers")
	assert.Empty(t, si.InstructionsFor("b4-model"), "the control: L has none")
}

func byID(items []study.Item) map[int64]study.Item {
	out := map[int64]study.Item{}
	for _, it := range items {
		out[it.ID] = it
	}
	return out
}

func TestRecall_ShowsWalkMidsOnlyAsPointersInLPtr(t *testing.T) {
	env := sharedStore(t)
	ctx := context.Background()
	plain, err := openModel(t, env, "b4-model").Recall(ctx, chainQuery)
	require.NoError(t, err)
	m := openModel(t, env, "b4-model-ptr")
	items, err := m.Recall(ctx, chainQuery)
	require.NoError(t, err)
	assert.Equal(t, itemIDs(plain), itemIDs(items), "L's items, in L's order")
	got := byID(items)
	assert.Equal(t, pointerText("gebind components"), got[2].Text)
	assert.Equal(t, pointerText("mateo deatreal"), got[3].Text)
	assert.NotContains(t, got[3].Text, "5504", "no walk-episode text")
	assert.Equal(t, int64(9), got[3].SourceFirst, "the pointer keeps the interval memory_inspect reads")
	assert.Equal(t, chainSummaries[0], got[1].Text, "the control: a similar mid keeps its summary")
	hits, err := m.Search(ctx, chainQuery, 5)
	require.NoError(t, err)
	for _, it := range hits {
		if it.Kind == "linked" {
			assert.True(t, strings.HasPrefix(it.Text, "Linked through"), "memory_search shows walk mids as pointers too")
		}
	}
	assert.Contains(t, study.Recalled(items)[5].Content, "[mid:3 sources:9-12;", "the rendered pointer names the mid and its interval")
}

func TestRecall_AttributesWalkMidsToTheirSubjectInLAttr(t *testing.T) {
	env := sharedStore(t)
	ctx := context.Background()
	m := openModel(t, env, "b4-model-attr")
	items, err := m.Recall(ctx, chainQuery)
	require.NoError(t, err)
	got := byID(items)
	assert.Equal(t, attribution("gebind components", []string{"rodend"})+chainSummaries[1], got[2].Text, "mid 2 was written about gebind components first")
	assert.Equal(t, `About "mateo deatreal"; the question names "rodend". `+chainSummaries[2], got[3].Text)
	assert.Equal(t, chainSummaries[0], got[1].Text, "the control: a similar mid carries no attribution")
	assert.Equal(t, map[int64]string{2: "gebind components", 3: "mateo deatreal"}, m.(*memory).LastTrace()["subjects"], "the trace names each walk mid's subject")
	// A walk mid whose subject is one of the query's entities is shown
	// as it is: the attribution marks only a difference.
	a := m.(*memory)
	assert.Equal(t, chainSummaries[2], a.shown(study.Item{ID: 3, Text: chainSummaries[2]}, "mateo deatreal", []string{"mateo deatreal", "rodend"}))
	assert.Equal(t, attribution("gebind components", []string{"mateo deatreal", "rodend"})+chainSummaries[1],
		a.shown(study.Item{ID: 2, Text: chainSummaries[1]}, "gebind components", []string{"mateo deatreal", "rodend"}))
}

// recordedEntities gives env the corpus of sharedStore's 60 messages and a
// complete entity recording for its 15 batches, the chain's names on the
// first three, so a model-entity setting builds without a call.
func recordedEntities(t *testing.T, env *study.Env) {
	t.Helper()
	env.Dir = t.TempDir()
	env.Corpus = &study.Corpus{}
	for i := 1; i <= 60; i++ {
		env.Corpus.Base = append(env.Corpus.Base, study.Message{ID: int64(i), Role: "user", Content: fmt.Sprintf("message %d", i)})
	}
	named := map[int64][]string{1: {"Rodend", "Gebind Components"}, 5: {"Gebind Components", "Mateo Deatreal"}, 9: {"Mateo Deatreal"}}
	var lines []string
	for first := int64(1); first <= 57; first += 4 {
		line, _ := json.Marshal(batchEntities{First: first, Last: first + 3, Entities: append([]string{}, named[first]...)})
		lines = append(lines, string(line))
	}
	path := entitiesPath(env, 4)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644))
}

func TestBuild_KeyPtrStartsFromCKeysStore(t *testing.T) {
	env := sharedStore(t)
	recordedEntities(t, env)
	ctx := context.Background()
	_, err := New().Build(ctx, env, keyed, filepath.Join(env.Work, "builds", "L", keyed))
	require.ErrorContains(t, err, "b4-w8-key", "C-key is built first")
	key := env.BuildDir("C", "b4-w8-key")
	require.NoError(t, study.CopyDir(filepath.Join(env.Work, "shared", "b4"), key))
	db, err := sql.Open("sqlite", filepath.Join(key, "memory.db"))
	require.NoError(t, err)
	_, err = db.Exec(`ALTER TABLE conversation_mid ADD COLUMN context TEXT`)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE conversation_mid SET context='At the Rodend site.' WHERE id=2`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	dir := filepath.Join(env.Work, "builds", "L", keyed)
	b, err := New().Build(ctx, env, keyed, dir)
	require.NoError(t, err)
	assert.Equal(t, len("At the Rodend site."), b.Notes["context_chars"])
	plain, err := New().Build(ctx, env, "b4-model-ptr", filepath.Join(env.Work, "builds", "L", "b4-model-ptr"))
	require.NoError(t, err)
	assert.Equal(t, plain.StoredChars+len("At the Rodend site."), b.StoredChars, "the keys' context counts as stored")
	var c string
	db, err = sql.Open("sqlite", filepath.Join(dir, "memory.db"))
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.QueryRow(`SELECT context FROM conversation_mid WHERE id=2`).Scan(&c))
	assert.Equal(t, "At the Rodend site.", c, "the build is a copy of C-key's store")
	m, err := New().Open(ctx, env, keyed, dir)
	require.NoError(t, err)
	defer m.Close()
	items, err := m.Recall(ctx, chainQuery)
	require.NoError(t, err)
	assert.Equal(t, pointerText("mateo deatreal"), byID(items)[3].Text, "C-key+L-ptr shows walk mids as L-ptr does")
}
