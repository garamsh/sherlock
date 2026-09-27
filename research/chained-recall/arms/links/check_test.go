package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/tool"
	"github.com/garamsh/sherlock/research/chained-recall/study"
)

// fakeCheck answers the grounding check with scripted responses, the last
// one repeated, and keeps every request.
type fakeCheck struct {
	answers []agent.Response
	err     error
	calls   [][]agent.Message
	tools   [][]tool.Descriptor
}

func (f *fakeCheck) Chat(_ context.Context, messages []agent.Message, tools []tool.Descriptor) (agent.Response, error) {
	f.calls = append(f.calls, messages)
	f.tools = append(f.tools, tools)
	if f.err != nil {
		return agent.Response{}, f.err
	}
	r := f.answers[0]
	if len(f.answers) > 1 {
		f.answers = f.answers[1:]
	}
	return r, nil
}

func naming(ids ...int64) agent.Response {
	input, _ := json.Marshal(map[string][]int64{"items": append([]int64{}, ids...)})
	return agent.Response{ToolCalls: []agent.ToolCall{{Name: checkTool, Input: input}}}
}

// openCheck is L+chk over the chain store, its check answered by f.
func openCheck(t *testing.T, f *fakeCheck) *checked {
	t.Helper()
	return &checked{memory: openModel(t, sharedStore(t), "b4-model").(*memory), model: f}
}

func recallIDs(t *testing.T, m study.Memory, query string) []int64 {
	t.Helper()
	items, err := m.Recall(context.Background(), query)
	require.NoError(t, err)
	return itemIDs(items)
}

func TestCheck_KeepsOnlyTheNamedItemsWhenTheyConnect(t *testing.T) {
	f := &fakeCheck{answers: []agent.Response{naming(1, 2, 3)}}
	c := openCheck(t, f)
	unchecked := recallIDs(t, c.memory, chainQuery)
	require.Equal(t, []int64{15, 14, 1, 2, 13, 3, 12}, unchecked, "the setup: L's block holds walk mids 2 and 3")
	items, err := c.Recall(context.Background(), chainQuery)
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 2, 3}, itemIDs(items), "only the named items, in the check's order")
	assert.Equal(t, chainSummaries[1], items[1].Text, "a kept item is shown as L shows it")
	require.Len(t, f.calls, 1, "one narrow call")
	var sent struct {
		Question string `json:"question"`
		Items    []struct {
			ID   int64  `json:"id"`
			Text string `json:"text"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal([]byte(f.calls[0][1].Content), &sent))
	assert.Equal(t, chainQuery, sent.Question)
	assert.Len(t, sent.Items, len(unchecked), "the check sees every mid of the block")
	assert.Equal(t, checkPrompt, f.calls[0][0].Content)
	assert.Equal(t, checkTool, f.tools[0][0].Name, "the reserved schema")
	rec := c.LastTrace()["check"].(checkRecord)
	assert.True(t, rec.Ran)
	assert.True(t, rec.Connected)
	assert.Equal(t, []int64{2, 3}, rec.Walk)
	assert.Equal(t, []int64{1, 2, 3}, rec.Kept)
}

func TestCheck_TellsTheTurnNoMemoryStatesTheFactWhenItNamesNone(t *testing.T) {
	c := openCheck(t, &fakeCheck{answers: []agent.Response{naming()}})
	items, err := c.Recall(context.Background(), chainQuery)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, study.Item{Level: "notice", Kind: "check", Text: noEvidence}, items[0])
	rendered := study.Recalled(items)
	assert.Equal(t, agent.RecalledMemory{ID: "notice", Content: noEvidence, Age: "check"}, rendered[0], "the notice carries no memory label")
}

func TestCheck_DropsANamedSetThatDoesNotConnect(t *testing.T) {
	for _, tc := range []struct {
		name  string
		named []int64
	}{
		{"the first item names no query entity", []int64{3}},
		{"a later item shares no entity with the one before", []int64{1, 3}},
		{"the walk mids alone", []int64{2, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := openCheck(t, &fakeCheck{answers: []agent.Response{naming(tc.named...)}})
			items, err := c.Recall(context.Background(), chainQuery)
			require.NoError(t, err)
			assert.Equal(t, noEvidence, items[0].Text)
			rec := c.LastTrace()["check"].(checkRecord)
			assert.Equal(t, tc.named, rec.Named, "the trace keeps what was named")
			assert.False(t, rec.Connected)
		})
	}
	// Control: the same store keeps a connected set, so the refusals above
	// come from the connectivity rule.
	c := openCheck(t, &fakeCheck{answers: []agent.Response{naming(1, 2)}})
	assert.Equal(t, []int64{1, 2}, recallIDs(t, c, chainQuery))
}

func TestCheck_DoesNotRunWithoutWalkMids(t *testing.T) {
	f := &fakeCheck{answers: []agent.Response{naming()}}
	c := openCheck(t, f)
	query := "What is the episode number?"
	assert.Equal(t, recallIDs(t, c.memory, query), recallIDs(t, c, query), "the block is L's, which is S's here")
	assert.Empty(t, f.calls)
	assert.False(t, c.LastTrace()["check"].(checkRecord).Ran)
}

func TestCheck_ChecksMemorySearchResultsHoldingWalkMids(t *testing.T) {
	f := &fakeCheck{answers: []agent.Response{naming()}}
	c := openCheck(t, f)
	plain, err := c.memory.Search(context.Background(), chainQuery, 5)
	require.NoError(t, err)
	require.Contains(t, itemIDs(plain), int64(2), "the setup: L's search holds a walk mid")
	items, err := c.Search(context.Background(), chainQuery, 5)
	require.NoError(t, err)
	assert.Equal(t, []study.Item{{Level: "notice", Kind: "check", Text: noEvidence}}, items)
	assert.Len(t, f.calls, 1)
	assert.Equal(t, "search", c.LastTrace()["check"].(checkRecord).Call)
}

func TestCheck_AsksAgainOnAMalformedAnswerAndThenFailsClosed(t *testing.T) {
	malformed := agent.Response{Content: "Items 1 and 2."}
	f := &fakeCheck{answers: []agent.Response{malformed, naming(9), naming(1)}}
	c := openCheck(t, f)
	assert.Equal(t, []int64{1}, recallIDs(t, c, chainQuery), "the third, well-formed answer is used")
	assert.Len(t, f.calls, 3)
	assert.Contains(t, f.calls[1][2].Content, "Attempt 1 was not usable")
	assert.Len(t, f.calls[2], 3, "each retry replaces the note, not appends to it")
	assert.Equal(t, 3, c.LastTrace()["check"].(checkRecord).Attempts)

	f = &fakeCheck{answers: []agent.Response{malformed}}
	c = openCheck(t, f)
	items, err := c.Recall(context.Background(), chainQuery)
	require.NoError(t, err)
	assert.Equal(t, noEvidence, items[0].Text)
	assert.Len(t, f.calls, checkAttempts)
	assert.True(t, c.LastTrace()["check"].(checkRecord).Malformed)
}

func TestCheck_ReturnsAProviderErrorSoTheProbeIsRunAgain(t *testing.T) {
	c := openCheck(t, &fakeCheck{err: errors.New("HTTP 429")})
	_, err := c.Recall(context.Background(), chainQuery)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429", "the live runner's infrastructure test reads the cause")
}

func TestParseCheck_RefusesWhatTheSchemaDoesNotAllow(t *testing.T) {
	delivered := []int64{1, 2, 3, 4}
	for _, tc := range []struct {
		name string
		resp agent.Response
	}{
		{"no call", agent.Response{Content: "none"}},
		{"another tool", agent.Response{ToolCalls: []agent.ToolCall{{Name: "memory_search", Input: json.RawMessage(`{"items":[]}`)}}}},
		{"more than three", naming(1, 2, 3, 4)},
		{"an undelivered item", naming(7)},
		{"an item twice", naming(1, 1)},
		{"no list", agent.Response{ToolCalls: []agent.ToolCall{{Name: checkTool, Input: json.RawMessage(`{}`)}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseCheck(tc.resp, delivered)
			assert.Error(t, err)
		})
	}
	named, err := parseCheck(naming(3, 1), delivered)
	require.NoError(t, err, "the control: a well-formed answer is read")
	assert.Equal(t, []int64{3, 1}, named)
}

func TestOpenChecked_LogsEveryLiveCheckUnderTheProbe(t *testing.T) {
	env := sharedStore(t)
	env.Dir, env.Proxy = t.TempDir(), "http://127.0.0.1:1"
	m := openModel(t, env, "b4-model").(*memory)
	offline, err := openChecked(env, m, "b4-model-chk", env.BuildDir("L", "b4-model-chk"))
	require.NoError(t, err)
	assert.Empty(t, offline.(*checked).probe, "the control: the offline build has no probe")
	assert.Nil(t, offline.(*checked).log)
	live, err := openChecked(env, m, "b4-model-chk", env.LiveDir("L", "b4-model-chk", "N-001"))
	require.NoError(t, err)
	c := live.(*checked)
	c.model = &fakeCheck{answers: []agent.Response{naming()}}
	_, err = c.Recall(context.Background(), chainQuery)
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(env.Dir, "evidence", "live", "L", "b4-model-chk", "checks.jsonl"))
	require.NoError(t, err)
	var rec checkRecord
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(string(data))), &rec))
	assert.Equal(t, "N-001", rec.Probe)
	assert.True(t, rec.Ran)
	assert.Equal(t, []int64{}, rec.Kept)
}

func TestConnects_FollowsStoredEntitiesFromTheQuerysEntity(t *testing.T) {
	m := openModel(t, sharedStore(t), "b4-model").(*memory)
	seeds := []string{"rodend"}
	assert.True(t, m.graph.connects([]int64{1, 2, 3}, seeds))
	assert.True(t, m.graph.connects([]int64{1}, seeds), "one item naming the query's entity")
	assert.False(t, m.graph.connects(nil, seeds), "an empty set connects nothing")
	assert.False(t, m.graph.connects([]int64{2, 1}, seeds), "the first item must name the query's entity")
	assert.False(t, m.graph.connects([]int64{1, 4}, seeds), "mid 4 names nothing mid 1 names")
}
