package gateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/memory/sqlite"
	"github.com/garamsh/gagent/internal/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type evidenceModel struct {
	inner agent.Model
	calls int
}

func (m *evidenceModel) Chat(ctx context.Context, messages []agent.Message, tools []tool.Descriptor) (agent.Response, error) {
	m.calls++
	if m.calls == 4 {
		found := false
		for _, message := range messages {
			if message.Role == agent.RoleTool && message.Name == agent.MemoryInspectTool && strings.Contains(message.Content, "Correction: my preferred telescope is the Vega-2 reflector.") {
				found = true
			}
		}
		if !found {
			return agent.Response{}, errors.New("exact source was not inspected before answer")
		}
	}
	return m.inner.Chat(ctx, messages, tools)
}

type conversationEvents struct{ events []agent.Event }

func (s *conversationEvents) Emit(_ context.Context, e agent.Event) { s.events = append(s.events, e) }

func TestConversation_ParaphraseCorrectionAndExactSource(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	first := agent.NewScriptedModel(
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: "memory_search_terms", Input: json.RawMessage(`{"phrases":["telescope"]}`)}}},
		writebackStep(`{"reply":"Noted.","memory_ops":[]}`),
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: "memory_search_terms", Input: json.RawMessage(`{"phrases":["telescope"]}`)}}},
		writebackStep(`{"reply":"Corrected.","memory_ops":[]}`),
	)
	f := &gateway.Factory{MemoryPath: path, NewModel: func(string) agent.Model { return first }, Tools: noTools(), MaxHistoryMessages: 2}
	rt, err := f.New(ctx, "a", nil)
	require.NoError(t, err)
	_, err = rt.Session.Run(ctx, "My preferred telescope is the Kepler-9 refractor.")
	require.NoError(t, err)
	_, err = rt.Session.Run(ctx, "Correction: my preferred telescope is the Vega-2 reflector.")
	require.NoError(t, err)
	require.LessOrEqual(t, len(rt.Session.History()), 4)
	require.NoError(t, rt.Close(ctx))
	store, err := sqlite.Open(path)
	require.NoError(t, err)
	hits, err := store.Search(ctx, []string{"preferred telescope"}, 5)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(hits), 2)
	assert.Contains(t, hits[0].Summary, "Vega-2", "the correction is offered first")
	short, err := store.Short(ctx, hits[0].ShortID)
	require.NoError(t, err)
	source, err := store.Source(ctx, short.SourceID)
	require.NoError(t, err)
	assert.Equal(t, "Correction: my preferred telescope is the Vega-2 reflector.", source.Content)
	require.NoError(t, store.Close())

	model := agent.NewScriptedModel(
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: "memory_search_terms", Input: json.RawMessage(`{"phrases":["preferred telescope","Vega reflector"]}`)}}},
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: agent.MemoryInspectTool, Input: json.RawMessage(`{"kind":"short","id":3}`)}}},
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: agent.MemoryInspectTool, Input: json.RawMessage(`{"kind":"source","id":3}`)}}},
		writebackStep(`{"reply":"You corrected it to the Vega-2 reflector; your exact words were: 'Correction: my preferred telescope is the Vega-2 reflector.'","memory_ops":[]}`),
	)
	events := &conversationEvents{}
	f.NewModel = func(string) agent.Model { return &evidenceModel{inner: model} }
	restarted, err := f.New(ctx, "a", events)
	require.NoError(t, err)
	reply, err := restarted.Session.Run(ctx, "What did I say exactly about the optical instrument I prefer?")
	require.NoError(t, err)
	assert.Contains(t, reply, "Vega-2 reflector")
	assert.NotContains(t, reply, "Kepler-9")
	var inspections int
	for _, e := range events.events {
		if e.Kind == agent.EventToolCall && e.ToolName == agent.MemoryInspectTool {
			require.False(t, e.ToolFailed)
			inspections++
		}
	}
	assert.Equal(t, 2, inspections, "the source-inspection tool actually ran")
	require.NoError(t, restarted.Close(ctx))
}

func TestConversation_UnsupportedQuestionHasNoEvidence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	model := agent.NewScriptedModel(
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: "memory_search_terms", Input: json.RawMessage(`{"phrases":["lunar launch date"]}`)}}},
		writebackStep(`{"reply":"I don't have a source for that.","memory_ops":[]}`),
	)
	events := &conversationEvents{}
	rt, err := (&gateway.Factory{MemoryPath: path, NewModel: func(string) agent.Model { return model }, Tools: noTools()}).New(ctx, "a", events)
	require.NoError(t, err)
	reply, err := rt.Session.Run(ctx, "When is the lunar launch?")
	require.NoError(t, err)
	assert.Equal(t, "I don't have a source for that.", reply)
	var outcome agent.RecallOutcome
	for _, e := range events.events {
		if e.Kind == agent.EventRecall {
			outcome = e.RecallOutcome
		}
		assert.NotEqual(t, agent.MemoryInspectTool, e.ToolName)
	}
	assert.Equal(t, agent.RecallEmpty, outcome)
	require.NoError(t, rt.Close(ctx))
}

func TestConversation_SearchesMultipleTopics(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "memory.db"))
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: "I prefer cedar tea."}, {Role: agent.RoleAssistant, Content: "Noted."}}))
	require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: "My desk is beside the east window."}, {Role: agent.RoleAssistant, Content: "Noted."}}))
	hits, err := store.Search(ctx, []string{"cedar tea", "east window"}, 5)
	require.NoError(t, err)
	var cedar, window bool
	for _, h := range hits {
		cedar = cedar || contains(h.Summary, "cedar")
		window = window || contains(h.Summary, "window")
	}
	assert.True(t, cedar)
	assert.True(t, window)
}
func contains(s, part string) bool { return strings.Contains(s, part) }

func TestConversation_QueuePreservesOriginalInboundID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	factory := &gateway.Factory{MemoryPath: path, NewModel: func(string) agent.Model {
		return agent.NewScriptedModel(
			agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: "memory_search_terms", Input: json.RawMessage(`{"phrases":["cedar"]}`)}}},
			writebackStep(`{"reply":"Noted.","memory_ops":[]}`),
		)
	}, Tools: noTools()}
	queue, err := gateway.NewQueue(factory, "a", nil)
	require.NoError(t, err)
	id, err := queue.Enqueue(context.Background(), "sender", "Cedar is my favorite tea.")
	require.NoError(t, err)
	store, err := sqlite.Open(path)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		sources, err := store.Recent(context.Background(), 10)
		if err != nil {
			return false
		}
		for _, source := range sources {
			if source.ExternalID == id && source.Content == "Cedar is my favorite tea." {
				return true
			}
		}
		return false
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, queue.Close())
	require.NoError(t, store.Close())
}

func TestConversation_RelatedCandidateDoesNotSupportMissingDate(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	seed, err := sqlite.Open(path)
	require.NoError(t, err)
	require.NoError(t, seed.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: "The lunar mission uses the Aurora capsule."}, {Role: agent.RoleAssistant, Content: "Noted."}}))
	require.NoError(t, seed.Close())
	model := agent.NewScriptedModel(
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: "memory_search_terms", Input: json.RawMessage(`{"phrases":["lunar mission launch date"]}`)}}},
		writebackStep(`{"reply":"I found the capsule name but no launch date in the available source.","memory_ops":[]}`),
	)
	events := &conversationEvents{}
	rt, err := (&gateway.Factory{MemoryPath: path, NewModel: func(string) agent.Model { return model }, Tools: noTools()}).New(ctx, "a", events)
	require.NoError(t, err)
	reply, err := rt.Session.Run(ctx, "When is the lunar mission launching?")
	require.NoError(t, err)
	assert.Contains(t, reply, "no launch date")
	var count int
	for _, e := range events.events {
		if e.Kind == agent.EventRecall {
			count = e.RecalledCount
		}
	}
	assert.Greater(t, count, 0, "a related candidate was retrieved, so this is an answer control, not an empty-search case")
	require.NoError(t, rt.Close(ctx))
}
