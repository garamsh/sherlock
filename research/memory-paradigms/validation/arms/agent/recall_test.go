package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
	"github.com/garamsh/sherlock/internal/tool"
	"github.com/garamsh/sherlock/research/chained-recall/study"
)

type constEmbedder struct{}

func (constEmbedder) Embed(context.Context, string) ([]float32, error) { return []float32{1, 0}, nil }

type fixedSummary struct{}

func (fixedSummary) Summarize(_ context.Context, updates []string) (string, error) {
	return fmt.Sprintf("summary of %d messages", len(updates)), nil
}

// scriptedModel answers each call with the next scripted response and
// records the tools each call offered.
type scriptedModel struct {
	steps   []agent.Response
	offered [][]string
}

func (s *scriptedModel) Chat(_ context.Context, _ []agent.Message, tools []tool.Descriptor) (agent.Response, error) {
	var names []string
	for _, d := range tools {
		names = append(names, d.Name)
	}
	s.offered = append(s.offered, names)
	i := len(s.offered) - 1
	if i >= len(s.steps) {
		return agent.Response{Content: "thinking"}, nil
	}
	return s.steps[i], nil
}

func call(name, input string) agent.Response {
	return agent.Response{ToolCalls: []agent.ToolCall{{ID: name, Name: name, Input: json.RawMessage(input)}}}
}

// open writes eight messages to a store and opens R's memory over it with
// the scripted model, recording the episode the step's calls are made for.
func open(t *testing.T, model *scriptedModel, episodes *[]string) *memory {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.OpenConversation(filepath.Join(t.TempDir(), "memory.db"), constEmbedder{})
	require.NoError(t, err)
	for i := range 8 {
		require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: fmt.Sprintf("message %d", i+1)}}, fixedSummary{}))
	}
	t.Cleanup(func() { _ = store.Close() })
	return &memory{
		ShippedMemory: study.ShippedMemory{Conversation: store},
		newModel: func(episode string) (agent.Model, error) {
			*episodes = append(*episodes, episode)
			return model, nil
		},
		probes: map[string]string{"Who sits at desk 4?": "C2-001"},
	}
}

func TestRecall_DeliversOnlyFactsWhoseEverySourceTheStepRead(t *testing.T) {
	model := &scriptedModel{steps: []agent.Response{
		call("memory_search", `{"query":"desk 4"}`),
		call("memory_inspect", `{"id":5}`),
		call("recall_result", `{"facts":[{"statement":"Ana sits at desk 4.","sources":[5]},{"statement":"Ana moved.","sources":[5,7]},{"statement":"Bo is away.","sources":[9]}]}`),
	}}
	var episodes []string
	m := open(t, model, &episodes)

	items, err := m.Recall(context.Background(), "Who sits at desk 4?")
	require.NoError(t, err)

	require.Len(t, items, 1)
	assert.Equal(t, "Ana sits at desk 4.", items[0].Text)
	assert.Equal(t, "[fact:1 from sources 5, each read by the recall step]", items[0].Label)
	assert.Equal(t, []string{"C2-001"}, episodes, "the step's calls are ledgered under the probe")
	tr := m.LastTrace()
	assert.Equal(t, 3, tr["loop_calls"])
	assert.Equal(t, "result", tr["ended"])
	assert.Equal(t, 1, tr["searches"])
	assert.Equal(t, []int64{5}, tr["inspected"])
	assert.Equal(t, 3, tr["returned"])
	assert.Len(t, tr["dropped"], 2, "a fact citing a source the step never read is dropped")
}

func TestCheckCitations_RefusesAFactWithoutSources(t *testing.T) {
	kept, dropped := CheckCitations([]Fact{{Statement: "a", Sources: nil}, {Statement: "b", Sources: []int64{2}}}, map[int64]bool{2: true})
	assert.Equal(t, []Fact{{Statement: "b", Sources: []int64{2}}}, kept, "the control: a fact whose source was read is kept")
	assert.Equal(t, []Fact{{Statement: "a", Sources: nil}}, dropped)
}

func TestRecall_OffersOnlyRecallResultOnTheLastCallAndSaysWhenNothingWasFound(t *testing.T) {
	model := &scriptedModel{}
	var episodes []string
	m := open(t, model, &episodes)

	items, err := m.Recall(context.Background(), "Who sits at desk 4?")
	require.NoError(t, err)

	require.Len(t, model.offered, maxLoopCalls, "the step stops at its bound")
	assert.Equal(t, []string{"recall_result"}, model.offered[maxLoopCalls-1])
	assert.Contains(t, model.offered[0], "memory_search", "the control: earlier calls offer the search")
	assert.Equal(t, "exhausted", m.LastTrace()["ended"])
	require.Len(t, items, 1)
	assert.Equal(t, "none", items[0].Kind)
	assert.Contains(t, items[0].Text, "found no stored evidence")
}

func TestRecall_AskedAgainAfterAMalformedResult(t *testing.T) {
	model := &scriptedModel{steps: []agent.Response{
		call("recall_result", `{"facts":[{"statement":"","sources":[1]}]}`),
		call("recall_result", `{"facts":[]}`),
	}}
	var episodes []string
	m := open(t, model, &episodes)

	items, err := m.Recall(context.Background(), "Who sits at desk 4?")
	require.NoError(t, err)
	assert.Equal(t, 2, m.LastTrace()["loop_calls"])
	assert.Equal(t, 1, m.LastTrace()["parse_errors"])
	assert.Equal(t, "none", items[0].Kind)
}

func TestRecall_RefusesATaskNoProbeAsks(t *testing.T) {
	var episodes []string
	_, err := open(t, &scriptedModel{}, &episodes).Recall(context.Background(), "Something else?")
	assert.Error(t, err)
}

func TestRecall_NamesTheProbeFromTheTaskTheLiveSessionPasses(t *testing.T) {
	model := &scriptedModel{steps: []agent.Response{call("recall_result", `{"facts":[]}`)}}
	var episodes []string
	m := open(t, model, &episodes)

	_, err := m.Recall(context.Background(), "[msg-C2-001] owner: Who sits at desk 4?")
	require.NoError(t, err)
	assert.Equal(t, []string{"C2-001"}, episodes)
	_, err = m.Recall(context.Background(), "[msg-C2-999] owner: Who sits at desk 4?")
	assert.Error(t, err, "the control: a prefix naming another probe than the query's is refused")
}
