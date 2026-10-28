package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/tool"
)

// fileStore is a stateful conversation store: it keeps the agent's files,
// the recorded turns and every search query, and answers searches and the
// walk with fixed hits.
type fileStore struct {
	agent.ConversationMemory
	files   map[string]string
	writes  []string
	turns   [][]agent.SourceMessage
	queries []string
	limits  []int
	hits    []agent.ConversationHit
	linked  agent.Linked
}

func (s *fileStore) AgentFiles(context.Context) (map[string]string, error) {
	out := map[string]string{}
	for path, content := range s.files {
		out[path] = content
	}
	return out, nil
}

func (s *fileStore) SetAgentFile(_ context.Context, path, content string) error {
	if s.files == nil {
		s.files = map[string]string{}
	}
	s.files[path] = content
	s.writes = append(s.writes, path)
	return nil
}

func (s *fileStore) RecordTurn(_ context.Context, sources []agent.SourceMessage, _ agent.ConversationSummarizer) error {
	s.turns = append(s.turns, sources)
	return nil
}

func (s *fileStore) Search(_ context.Context, query string, limit int) ([]agent.ConversationHit, error) {
	s.queries = append(s.queries, query)
	s.limits = append(s.limits, limit)
	return s.hits, nil
}

func (s *fileStore) Linked(context.Context, string) (agent.Linked, error) { return s.linked, nil }

// recordingModel scripts responses and keeps what each call was shown.
type recordingModel struct {
	inner *agent.ScriptedModel
	seen  [][]agent.Message
	tools [][]tool.Descriptor
}

func (m *recordingModel) Chat(ctx context.Context, messages []agent.Message, tools []tool.Descriptor) (agent.Response, error) {
	m.seen = append(m.seen, append([]agent.Message(nil), messages...))
	m.tools = append(m.tools, tools)
	return m.inner.Chat(ctx, messages, tools)
}

func searchCall(query string) agent.ToolCall {
	input, _ := json.Marshal(map[string]string{"query": query})
	return agent.ToolCall{Name: agent.MemorySearchTool, Input: input}
}

func TestSessionRun_SearchRanksWithTheModelsQuery(t *testing.T) {
	store := &fileStore{hits: []agent.ConversationHit{{ID: 7, Summary: " The Lisbon offsite is booked for 42 people. ", SourceFirst: 25, SourceLast: 28}}}
	model := &recordingModel{inner: agent.NewScriptedModel(
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{searchCall("Lisbon offsite headcount")}},
		finalStep("42 people."),
	)}
	s := agent.NewSession(model, newFakeClient(), agent.Options{Conversation: store})

	reply, err := s.Run(context.Background(), "How many did I say to book it for?")
	require.NoError(t, err)
	assert.Equal(t, "42 people.", reply)

	assert.Equal(t, []string{"Lisbon offsite headcount"}, store.queries, "the model's query, not the task text, is ranked")
	assert.Equal(t, []int{5}, store.limits, "one search offers as many episodes as pre-turn similarity does")
	var result []map[string]any
	last := model.seen[1][len(model.seen[1])-1]
	require.Equal(t, agent.RoleTool, last.Role)
	require.NoError(t, json.Unmarshal([]byte(last.Content), &result))
	require.Len(t, result, 1)
	assert.Equal(t, map[string]any{"mid": float64(7), "summary": "The Lisbon offsite is booked for 42 people.", "source_first": float64(25), "source_last": float64(28)}, result[0])
}

func TestSessionRun_SearchTakesSimilarAndWalkEpisodesInTurn(t *testing.T) {
	episode := func(id int64) agent.ConversationHit {
		return agent.ConversationHit{ID: id, Summary: fmt.Sprintf("episode %d", id), SourceFirst: 4*id - 3, SourceLast: 4 * id}
	}
	store := &fileStore{
		hits: []agent.ConversationHit{episode(7), episode(8), episode(9)},
		linked: agent.Linked{Seeds: []string{"rodend"}, Hits: []agent.LinkedHit{
			{ConversationHit: episode(8), Subject: "gebind components"},
			{ConversationHit: episode(3), Subject: "rodend"},
			{ConversationHit: episode(4), Subject: "mateo deatreal"},
			{ConversationHit: episode(5), Subject: "office"},
		}},
	}
	model := &recordingModel{inner: agent.NewScriptedModel(
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{searchCall("Rodend order phone number")}},
		finalStep("done"),
	)}
	_, err := agent.NewSession(model, newFakeClient(), agent.Options{Conversation: store}).Run(context.Background(), "task")
	require.NoError(t, err)

	var result []struct {
		ID      int64  `json:"mid"`
		Summary string `json:"summary"`
	}
	require.NoError(t, json.Unmarshal([]byte(model.seen[1][len(model.seen[1])-1].Content), &result))
	var ids []int64
	for _, r := range result {
		ids = append(ids, r.ID)
	}
	assert.Equal(t, []int64{7, 8, 9, 3, 4}, ids, "similar and walk in turn, a repeat skipped, five in all")
	assert.Equal(t, `About "gebind components"; the question names "rodend". episode 8`, result[1].Summary)
	assert.Equal(t, "episode 3", result[3].Summary, "an episode about the queried entity is shown plain")
	assert.Equal(t, `About "mateo deatreal"; the question names "rodend". episode 4`, result[4].Summary)
	assert.Equal(t, "episode 7", result[0].Summary, "the control: a similar episode is shown plain")
}

func TestSessionRun_SearchRefusesAnEmptyQuery(t *testing.T) {
	store := &fileStore{}
	model := &recordingModel{inner: agent.NewScriptedModel(
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{searchCall("   ")}},
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{searchCall("offsite")}},
		finalStep("done"),
	)}
	events := &fakeEvents{}
	s := agent.NewSession(model, newFakeClient(), agent.Options{Conversation: store, Events: events})

	_, err := s.Run(context.Background(), "task")
	require.NoError(t, err)
	assert.Equal(t, []string{"offsite"}, store.queries, "the blank query never reaches the store; the control does")
	var failed []bool
	for _, e := range events.events {
		if e.Kind == agent.EventToolCall {
			failed = append(failed, e.ToolFailed)
		}
	}
	assert.Equal(t, []bool{true, false}, failed)
}

// TestSessionRun_SearchNeedsConversationMemory offers the search call only
// to a session with a store to search, the way inspection is offered.
func TestSessionRun_SearchNeedsConversationMemory(t *testing.T) {
	names := func(opts agent.Options) []string {
		model := &recordingModel{inner: agent.NewScriptedModel(finalStep("ok"))}
		_, err := agent.NewSession(model, newFakeClient(), opts).Run(context.Background(), "task")
		require.NoError(t, err)
		var out []string
		for _, d := range model.tools[0] {
			out = append(out, d.Name)
		}
		return out
	}
	assert.NotContains(t, names(agent.Options{}), agent.MemorySearchTool)
	assert.Contains(t, names(agent.Options{Conversation: &fileStore{}}), agent.MemorySearchTool)
}
