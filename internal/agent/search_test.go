package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/tool"
)

// noteStore is a stateful conversation store: it keeps the note, the
// recorded turns and every search query, and answers searches with fixed
// hits.
type noteStore struct {
	agent.ConversationMemory
	note     string
	setNotes []string
	turns    [][]agent.SourceMessage
	queries  []string
	limits   []int
	hits     []agent.ConversationHit
}

func (s *noteStore) Note(context.Context) (string, error) { return s.note, nil }

func (s *noteStore) SetNote(_ context.Context, note string) error {
	s.note = note
	s.setNotes = append(s.setNotes, note)
	return nil
}

func (s *noteStore) RecordTurn(_ context.Context, sources []agent.SourceMessage, _ agent.ConversationSummarizer) error {
	s.turns = append(s.turns, sources)
	return nil
}

func (s *noteStore) Search(_ context.Context, query string, limit int) ([]agent.ConversationHit, error) {
	s.queries = append(s.queries, query)
	s.limits = append(s.limits, limit)
	return s.hits, nil
}

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
	store := &noteStore{hits: []agent.ConversationHit{{ID: 7, Summary: " The Lisbon offsite is booked for 42 people. ", SourceFirst: 25, SourceLast: 28}}}
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

func TestSessionRun_SearchRefusesAnEmptyQuery(t *testing.T) {
	store := &noteStore{}
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
	assert.Contains(t, names(agent.Options{Conversation: &noteStore{}}), agent.MemorySearchTool)
}
