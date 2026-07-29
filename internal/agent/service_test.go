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

// failingTool always fails, returning the next scripted error message (the
// last one repeats), and counts how often it was actually executed.
type failingTool struct {
	errs  []string
	calls int
}

func (t *failingTool) Name() string                  { return "flaky" }
func (t *failingTool) Description() string           { return "always fails" }
func (t *failingTool) InputSchema() json.RawMessage  { return nil }
func (t *failingTool) OutputSchema() json.RawMessage { return nil }

func (t *failingTool) Call(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
	msg := t.errs[len(t.errs)-1]
	if t.calls < len(t.errs) {
		msg = t.errs[t.calls]
	}
	t.calls++
	return nil, errors.New(msg)
}

// stubbornModel re-issues one failing tool call until a tool result tells it
// the call was not executed, then answers.
type stubbornModel struct {
	call agent.ToolCall
}

func (m *stubbornModel) Chat(_ context.Context, messages []agent.Message, _ []tool.Descriptor) (agent.Response, error) {
	last := messages[len(messages)-1]
	if last.Role == agent.RoleTool && strings.Contains(last.Content, "Not executed") {
		return agent.Response{Content: "changing approach\n```memory\n{}\n```"}, nil
	}
	return agent.Response{
		Content:   "retrying\n```memory\n{}\n```",
		ToolCalls: []agent.ToolCall{m.call},
	}, nil
}

func TestSessionRun_RepeatedFailingCallShortCircuits(t *testing.T) {
	flaky := &failingTool{errs: []string{"boom"}}
	model := &stubbornModel{call: agent.ToolCall{Name: "flaky", Input: json.RawMessage(`{"n":1}`)}}
	events := &fakeEvents{}
	s := agent.NewSession(model, registryWith(t, flaky), agent.Options{Events: events})

	final, err := s.Run(context.Background(), "task")
	require.NoError(t, err)
	assert.Equal(t, "changing approach", final, "the turn reaches a final response instead of exhausting the loop")
	assert.Equal(t, 2, flaky.calls, "the identical failing call is executed twice, then short-circuited")

	// The short-circuited attempt is fed back as a failed tool result and
	// stays visible on the monitoring stream.
	toolMsg := s.History()[len(s.History())-2]
	assert.Equal(t, agent.RoleTool, toolMsg.Role)
	assert.Contains(t, toolMsg.Content, "you already made this exact call")
	assert.Contains(t, toolMsg.Content, "boom")
	var toolEvents []agent.Event
	for _, e := range events.events {
		if e.Kind == agent.EventToolCall {
			toolEvents = append(toolEvents, e)
		}
	}
	require.Len(t, toolEvents, 3)
	last := toolEvents[2]
	assert.True(t, last.ToolFailed)
	assert.Contains(t, last.ToolResult, "you already made this exact call")
	assert.JSONEq(t, `{"n":1}`, string(last.ToolInput))
}

func TestSessionRun_RepeatDetectionIsExactMatch(t *testing.T) {
	tests := []struct {
		name  string
		errs  []string
		calls []agent.ToolCall
	}{
		{
			name: "same name different input",
			errs: []string{"boom"},
			calls: []agent.ToolCall{
				{Name: "flaky", Input: json.RawMessage(`{"n":1}`)},
				{Name: "flaky", Input: json.RawMessage(`{"n":1}`)},
				{Name: "flaky", Input: json.RawMessage(`{"n":2}`)},
			},
		},
		{
			name: "same call different error",
			errs: []string{"boom one", "boom two", "boom three"},
			calls: []agent.ToolCall{
				{Name: "flaky", Input: json.RawMessage(`{"n":1}`)},
				{Name: "flaky", Input: json.RawMessage(`{"n":1}`)},
				{Name: "flaky", Input: json.RawMessage(`{"n":1}`)},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			steps := make([]agent.ScriptedStep, 0, len(tc.calls)+1)
			for _, call := range tc.calls {
				steps = append(steps, agent.ScriptedStep{
					Content:   "calling\n```memory\n{}\n```",
					ToolCalls: []agent.ToolCall{call},
				})
			}
			steps = append(steps, agent.ScriptedStep{Content: "done\n```memory\n{}\n```"})
			flaky := &failingTool{errs: tc.errs}
			s := agent.NewSession(agent.NewScriptedModel(steps...), registryWith(t, flaky), agent.Options{})

			final, err := s.Run(context.Background(), "task")
			require.NoError(t, err)
			assert.Equal(t, "done", final)
			assert.Equal(t, len(tc.calls), flaky.calls, "every call differing in name, input or error executes")
			for _, msg := range s.History() {
				assert.NotContains(t, msg.Content, "Not executed", "no call was short-circuited")
			}
		})
	}
}

func TestSessionRun_RepeatBookkeepingResetsPerTurn(t *testing.T) {
	flaky := &failingTool{errs: []string{"boom"}}
	call := agent.ToolCall{Name: "flaky", Input: json.RawMessage(`{"n":1}`)}
	step := agent.ScriptedStep{Content: "calling\n```memory\n{}\n```", ToolCalls: []agent.ToolCall{call}}
	final := agent.ScriptedStep{Content: "done\n```memory\n{}\n```"}
	s := agent.NewSession(agent.NewScriptedModel(step, step, final, step, step, final), registryWith(t, flaky), agent.Options{})

	_, err := s.Run(context.Background(), "task one")
	require.NoError(t, err)
	require.Equal(t, 2, flaky.calls)

	_, err = s.Run(context.Background(), "task two")
	require.NoError(t, err)
	assert.Equal(t, 4, flaky.calls, "the second turn starts with empty repeat bookkeeping")
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

func TestSessionRun_FailureEmitsTerminalEvent(t *testing.T) {
	model := agent.NewScriptedModel(
		agent.ScriptedStep{Err: errors.New("provider down")},
	)
	events := &fakeEvents{}
	s := agent.NewSession(model, tool.NewRegistry(), agent.Options{Events: events})

	_, err := s.Run(context.Background(), "task")
	require.Error(t, err)

	// A failed turn still terminates visibly on the monitoring stream.
	require.Equal(t, []agent.EventKind{
		agent.EventTurnStarted,
		agent.EventTurnFailed,
	}, events.kinds())
	terminal := events.events[len(events.events)-1]
	assert.Equal(t, 1, terminal.Turn)
	assert.Contains(t, terminal.Error, "provider down")
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
