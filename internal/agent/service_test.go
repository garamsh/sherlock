package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/tool"
	"github.com/garamsh/gagent/internal/tool/mocks"
)

// fakeRecaller scripts the recall hook: it returns fixed memories and
// records the task statements it was queried with.
type fakeRecaller struct {
	memories []agent.RecalledMemory
	tasks    []string
	err      error
}

func (f *fakeRecaller) Recall(_ context.Context, task string) ([]agent.RecalledMemory, error) {
	f.tasks = append(f.tasks, task)
	return f.memories, f.err
}

// fakeWriteback records every batch of parsed memory operations.
type fakeWriteback struct {
	batches [][]agent.WritebackOp
}

func (f *fakeWriteback) Apply(_ context.Context, ops []agent.WritebackOp) error {
	f.batches = append(f.batches, ops)
	return nil
}

// fakeEvents records the emitted events in order.
type fakeEvents struct {
	events []agent.Event
}

func (f *fakeEvents) Emit(_ context.Context, e agent.Event) {
	f.events = append(f.events, e)
}

func (f *fakeEvents) kinds() []agent.EventKind {
	kinds := make([]agent.EventKind, len(f.events))
	for i, e := range f.events {
		kinds[i] = e.Kind
	}
	return kinds
}

// fakeCompactor records invocations and drops all but the newest message.
type fakeCompactor struct {
	calls int
}

func (f *fakeCompactor) Compact(_ context.Context, history []agent.Message) ([]agent.Message, error) {
	f.calls++
	return history[len(history)-1:], nil
}

// newEchoTool returns a mock tool (mockery) that echoes its input text.
// The scenario documents the scripted mock model contract (ADR 0005): each
// Chat call consumes exactly one scripted step, in order.
func newEchoTool(t *testing.T) *mocks.MockTool {
	t.Helper()
	echo := mocks.NewMockTool(t)
	echo.EXPECT().Name().Return("echo")
	echo.EXPECT().Description().Return("echo the text back")
	echo.EXPECT().InputSchema().Return(json.RawMessage(nil))
	echo.EXPECT().OutputSchema().Return(json.RawMessage(nil))
	echo.EXPECT().Call(mock.Anything, json.RawMessage(`{"text":"hi"}`)).
		Return(json.RawMessage(`{"echo":"hi"}`), nil)
	return echo
}

func registryWith(t *testing.T, tools ...tool.Tool) *tool.Registry {
	t.Helper()
	r := tool.NewRegistry()
	for _, tt := range tools {
		require.NoError(t, r.Register(tt))
	}
	return r
}

func TestSessionRun_ScriptedMultiTurnScenario(t *testing.T) {
	// Script: turn 1 the model requests the echo tool and keeps/adds
	// memories; turn 2 it answers with the tool result and updates/deletes.
	model := agent.NewScriptedModel(
		agent.ScriptedStep{
			Content: "Let me check.\n```memory\n{\"keep\":[\"m1\"],\"add\":[{\"content\":\"user likes tea\",\"type\":\"semantic\"}]}\n```",
			ToolCalls: []agent.ToolCall{
				{Name: "echo", Input: json.RawMessage(`{"text":"hi"}`)},
			},
		},
		agent.ScriptedStep{
			Content: "The echo said hi.\n```memory\n{\"update\":[{\"id\":\"m1\",\"content\":\"revised\"}],\"delete\":[\"m9\"]}\n```",
		},
	)
	recaller := &fakeRecaller{memories: []agent.RecalledMemory{
		{ID: "m1", Content: "user drinks tea", Age: "2d"},
	}}
	writeback := &fakeWriteback{}
	events := &fakeEvents{}

	s := agent.NewSession(model, registryWith(t, newEchoTool(t)), agent.Options{
		Recaller:  recaller,
		Writeback: writeback,
		Events:    events,
	})

	final, err := s.Run(context.Background(), "what did the echo say?")
	require.NoError(t, err)
	assert.Equal(t, "The echo said hi.", final)

	// Frame and recall anchored the turn, behind the injected instructions.
	require.Equal(t, []string{"what did the echo say?"}, recaller.tasks)
	history := s.History()
	require.Len(t, history, 6)
	assert.Equal(t, agent.RoleSystem, history[0].Role)
	assert.Equal(t, agent.ComposeInstructions(""), history[0].Content)
	assert.Equal(t, agent.RoleSystem, history[1].Role)
	assert.Contains(t, history[1].Content, "## Current task\nwhat did the echo say?")
	assert.Contains(t, history[1].Content, "- [m1] (2d): user drinks tea")
	assert.Equal(t, agent.RoleUser, history[2].Role)
	assert.Equal(t, agent.RoleAssistant, history[3].Role)
	require.Len(t, history[3].ToolCalls, 1)
	assert.Equal(t, "echo", history[3].ToolCalls[0].Name)
	assert.NotEmpty(t, history[3].ToolCalls[0].ID, "scripted model assigns tool-call IDs")

	// The tool result was incorporated before the final response.
	assert.Equal(t, agent.RoleTool, history[4].Role)
	assert.Equal(t, `{"echo":"hi"}`, history[4].Content)
	assert.Equal(t, history[3].ToolCalls[0].ID, history[4].ToolCallID)
	assert.Equal(t, agent.RoleAssistant, history[5].Role)

	// Every model response's memory block reached the writeback sink.
	require.Equal(t, [][]agent.WritebackOp{
		{
			{Kind: agent.OpKeep, ID: "m1"},
			{Kind: agent.OpAdd, Content: "user likes tea", Type: "semantic"},
		},
		{
			{Kind: agent.OpUpdate, ID: "m1", Content: "revised"},
			{Kind: agent.OpDelete, ID: "m9"},
		},
	}, writeback.batches)

	// Events fired in order within the turn.
	require.Equal(t, []agent.EventKind{
		agent.EventTurnStarted,
		agent.EventWriteback,
		agent.EventToolCall,
		agent.EventWriteback,
		agent.EventTurnFinished,
	}, events.kinds())
	assert.Equal(t, "what did the echo say?", events.events[0].Task)
	toolEvent := events.events[2]
	assert.Equal(t, "echo", toolEvent.ToolName)
	assert.JSONEq(t, `{"text":"hi"}`, string(toolEvent.ToolInput))
	assert.Equal(t, `{"echo":"hi"}`, toolEvent.ToolResult)
	assert.False(t, toolEvent.ToolFailed)
	assert.Equal(t, "The echo said hi.", events.events[4].Response)
	for _, e := range events.events {
		assert.Equal(t, 1, e.Turn)
	}
}

func TestSessionRun_RollingSession(t *testing.T) {
	model := agent.NewScriptedModel(
		agent.ScriptedStep{Content: "first\n```memory\n{}\n```"},
		agent.ScriptedStep{Content: "second\n```memory\n{}\n```"},
	)
	events := &fakeEvents{}
	s := agent.NewSession(model, tool.NewRegistry(), agent.Options{Events: events})

	first, err := s.Run(context.Background(), "task one")
	require.NoError(t, err)
	second, err := s.Run(context.Background(), "task two")
	require.NoError(t, err)
	assert.Equal(t, "first", first)
	assert.Equal(t, "second", second)

	// One rolling session: history accumulates, turn numbers increment.
	assert.Len(t, s.History(), 7)
	assert.Equal(t, 1, events.events[0].Turn)
	last := events.events[len(events.events)-1]
	assert.Equal(t, agent.EventTurnFinished, last.Kind)
	assert.Equal(t, 2, last.Turn)
}

func TestSessionRun_NoRecallStatesAbsence(t *testing.T) {
	model := agent.NewScriptedModel(
		agent.ScriptedStep{Content: "ok\n```memory\n{}\n```"},
	)
	s := agent.NewSession(model, tool.NewRegistry(), agent.Options{})

	_, err := s.Run(context.Background(), "anything")
	require.NoError(t, err)
	assert.Contains(t, s.History()[1].Content, "No relevant memories.")
}

func TestSessionRun_MalformedWriteback(t *testing.T) {
	model := agent.NewScriptedModel(
		agent.ScriptedStep{Content: "broken\n```memory\nnot json\n```"},
	)
	s := agent.NewSession(model, tool.NewRegistry(), agent.Options{})

	_, err := s.Run(context.Background(), "task")
	require.Error(t, err)
	assert.True(t, errors.Is(err, agent.ErrMalformedWriteback))
}

func TestSessionRun_UnknownToolFeedsErrorBack(t *testing.T) {
	model := agent.NewScriptedModel(
		agent.ScriptedStep{
			Content:   "calling\n```memory\n{}\n```",
			ToolCalls: []agent.ToolCall{{Name: "ghost", Input: json.RawMessage(`{}`)}},
		},
		agent.ScriptedStep{Content: "recovered\n```memory\n{}\n```"},
	)
	events := &fakeEvents{}
	s := agent.NewSession(model, tool.NewRegistry(), agent.Options{Events: events})

	final, err := s.Run(context.Background(), "task")
	require.NoError(t, err)
	assert.Equal(t, "recovered", final)

	toolMsg := s.History()[4]
	assert.Equal(t, agent.RoleTool, toolMsg.Role)
	assert.Contains(t, toolMsg.Content, "tool not found")
	toolEvent := events.events[2]
	assert.Equal(t, agent.EventToolCall, toolEvent.Kind)
	assert.True(t, toolEvent.ToolFailed)
	assert.Contains(t, toolEvent.ToolResult, "tool not found")
}

func TestSessionRun_ScriptExhausted(t *testing.T) {
	model := agent.NewScriptedModel(
		agent.ScriptedStep{
			Content:   "loop\n```memory\n{}\n```",
			ToolCalls: []agent.ToolCall{{Name: "ghost", Input: json.RawMessage(`{}`)}},
		},
	)
	s := agent.NewSession(model, tool.NewRegistry(), agent.Options{})

	_, err := s.Run(context.Background(), "task")
	require.Error(t, err)
	assert.True(t, errors.Is(err, agent.ErrScriptExhausted))
}

func TestSessionRun_LoopExhausted(t *testing.T) {
	model := agent.NewScriptedModel(
		agent.ScriptedStep{
			Content:   "again\n```memory\n{}\n```",
			ToolCalls: []agent.ToolCall{{Name: "ghost", Input: json.RawMessage(`{}`)}},
		},
		agent.ScriptedStep{
			Content:   "again\n```memory\n{}\n```",
			ToolCalls: []agent.ToolCall{{Name: "ghost", Input: json.RawMessage(`{}`)}},
		},
	)
	s := agent.NewSession(model, tool.NewRegistry(), agent.Options{MaxIterations: 2})

	_, err := s.Run(context.Background(), "task")
	require.Error(t, err)
	assert.True(t, errors.Is(err, agent.ErrLoopExhausted))
}

func TestSessionRun_CompactorHook(t *testing.T) {
	model := agent.NewScriptedModel(
		agent.ScriptedStep{Content: "one\n```memory\n{}\n```"},
		agent.ScriptedStep{Content: "two\n```memory\n{}\n```"},
	)
	compactor := &fakeCompactor{}
	s := agent.NewSession(model, tool.NewRegistry(), agent.Options{
		Compactor:          compactor,
		MaxHistoryMessages: 3,
	})

	_, err := s.Run(context.Background(), "task one")
	require.NoError(t, err)
	_, err = s.Run(context.Background(), "task two")
	require.NoError(t, err)

	assert.Positive(t, compactor.calls, "compactor hook fires once history exceeds the cap")
	assert.LessOrEqual(t, len(s.History()), 4, "history stays bounded after compaction")
}

func TestSessionRun_ModelErrorPropagates(t *testing.T) {
	model := agent.NewScriptedModel(
		agent.ScriptedStep{Err: errors.New("provider down")},
	)
	s := agent.NewSession(model, tool.NewRegistry(), agent.Options{})

	_, err := s.Run(context.Background(), "task")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider down")
}

// captureModel records the messages of every Chat call so tests can assert
// what the session sent the model.
type captureModel struct {
	calls [][]agent.Message
}

func (m *captureModel) Chat(_ context.Context, messages []agent.Message, _ []tool.Descriptor) (agent.Response, error) {
	m.calls = append(m.calls, append([]agent.Message(nil), messages...))
	return agent.Response{Content: "done"}, nil
}

func TestSessionRun_InstructionsAreFirstSystemMessage(t *testing.T) {
	model := &captureModel{}
	s := agent.NewSession(model, tool.NewRegistry(), agent.Options{})

	_, err := s.Run(context.Background(), "task")
	require.NoError(t, err)

	messages := model.calls[0]
	require.GreaterOrEqual(t, len(messages), 3)
	assert.Equal(t, agent.RoleSystem, messages[0].Role)
	assert.Equal(t, agent.ComposeInstructions(""), messages[0].Content)
	assert.Equal(t, agent.RoleSystem, messages[1].Role, "frame context follows the instructions")
	assert.Equal(t, agent.RoleUser, messages[2].Role)
}

func TestSessionRun_CustomEgoKeepsContract(t *testing.T) {
	model := &captureModel{}
	s := agent.NewSession(model, tool.NewRegistry(), agent.Options{Ego: "You are a test ego."})

	_, err := s.Run(context.Background(), "task")
	require.NoError(t, err)

	instructions := model.calls[0][0].Content
	assert.True(t, strings.HasPrefix(instructions, "You are a test ego."), "ego leads the instructions")
	assert.Contains(t, instructions, agent.Contract, "platform contract survives a custom ego")
}

func TestSessionRun_InstructionsInjectedOnce(t *testing.T) {
	model := &captureModel{}
	s := agent.NewSession(model, tool.NewRegistry(), agent.Options{})

	_, err := s.Run(context.Background(), "task one")
	require.NoError(t, err)
	_, err = s.Run(context.Background(), "task two")
	require.NoError(t, err)

	second := model.calls[1]
	var injections int
	for _, msg := range second {
		if msg.Content == agent.ComposeInstructions("") {
			injections++
		}
	}
	assert.Equal(t, 1, injections, "instructions are injected once, on the first turn")
	require.Len(t, model.calls, 2)
}
