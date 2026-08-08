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

// fakeWriteback records every batch of parsed memory operations and
// returns the canned coherence set.
type fakeWriteback struct {
	batches [][]agent.WritebackOp
	similar []agent.RecalledMemory
}

func (f *fakeWriteback) Apply(_ context.Context, ops []agent.WritebackOp) ([]agent.RecalledMemory, error) {
	f.batches = append(f.batches, ops)
	return f.similar, nil
}

// recordingModel captures the messages each Chat call sees.
type recordingModel struct {
	steps []agent.ScriptedStep
	seen  [][]agent.Message
}

func (m *recordingModel) Chat(_ context.Context, msgs []agent.Message, _ []tool.Descriptor) (agent.Response, error) {
	m.seen = append(m.seen, append([]agent.Message(nil), msgs...))
	if len(m.steps) == 0 {
		return agent.Response{Content: "done.\n```memory\n{}\n```"}, nil
	}
	step := m.steps[0]
	m.steps = m.steps[1:]
	return agent.Response{Content: step.Content, ToolCalls: step.ToolCalls}, nil
}

// TestSession_WriteTimeCoherencePresentedToModel pins ADR 0006 step 5:
// the memories similar to a writeback add join the history as a system
// note, and the next model call reads them for judgment.
func TestSession_WriteTimeCoherencePresentedToModel(t *testing.T) {
	model := &recordingModel{steps: []agent.ScriptedStep{
		{Content: "Noted.\n```memory\n{\"add\":[{\"content\":\"user's favorite telescope is the Orion XT8\",\"type\":\"semantic\"}]}\n```"},
		{Content: "Corrected the old one.\n```memory\n{}\n```"},
	}}
	writeback := &fakeWriteback{similar: []agent.RecalledMemory{
		{ID: "7", Content: "user's favorite telescope is the Kepler-9 refractor", Age: "3d"},
	}}

	s := agent.NewSession(model, tool.NewRegistry(), agent.Options{Writeback: writeback})
	_, err := s.Run(context.Background(), "my favorite telescope is the Orion XT8")
	require.NoError(t, err)

	var note string
	for _, m := range s.History() {
		if m.Role == agent.RoleSystem && strings.Contains(m.Content, "## Memory coherence") {
			note = m.Content
		}
	}
	require.NotEmpty(t, note, "the coherence note joins the history")
	assert.Contains(t, note, "Kepler-9 refractor")
	assert.Contains(t, note, "[7]")

	// The next turn's model call sees the note in its context.
	_, err = s.Run(context.Background(), "anything else?")
	require.NoError(t, err)
	require.Len(t, model.seen, 2)
	var seenByModel bool
	for _, m := range model.seen[1] {
		if strings.Contains(m.Content, "## Memory coherence") {
			seenByModel = true
		}
	}
	assert.True(t, seenByModel, "the model reads the coherence note at its next call")
}

// TestSession_NoCoherenceNoteWithoutSimilar pins the quiet path: nothing
// similar returned, no note in the history.
func TestSession_NoCoherenceNoteWithoutSimilar(t *testing.T) {
	model := &recordingModel{steps: []agent.ScriptedStep{
		{Content: "Done.\n```memory\n{}\n```"},
	}}
	s := agent.NewSession(model, tool.NewRegistry(), agent.Options{Writeback: &fakeWriteback{}})

	_, err := s.Run(context.Background(), "hello")
	require.NoError(t, err)
	for _, m := range s.History() {
		assert.NotContains(t, m.Content, "## Memory coherence")
	}
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
// The scenario documents the scripted mock model contract (ADR 0004): each
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

// TestSessionRun_LenientWritebackParse is issue #93's regression. A model
// response whose ```memory block is unparseable (unterminated fence or
// invalid JSON) must not fail the turn: the visible reply still reaches
// the user, the memory update is dropped for that turn only, and the
// session records the parse miss on the event stream as
// EventWritebackError.
func TestSessionRun_LenientWritebackParse(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		wantWarnSubstr string
	}{
		{"unterminated fence", "still useful\n```memory\n{}", "unterminated"},
		{"invalid JSON", "still useful\n```memory\nnot json\n```", "invalid JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := agent.NewScriptedModel(agent.ScriptedStep{Content: tt.content})
			events := &fakeEvents{}
			writeback := &fakeWriteback{}
			s := agent.NewSession(model, tool.NewRegistry(), agent.Options{
				Events:    events,
				Writeback: writeback,
			})

			final, err := s.Run(context.Background(), "task")
			require.NoError(t, err, "parse failure is non-fatal (issue #93)")
			assert.Equal(t, "still useful", final, "visible reply reaches the user")

			// The writeback sink receives no operations for this turn.
			assert.Empty(t, writeback.batches,
				"the memory update for this turn is dropped")

			// The monitoring stream records the parse miss.
			var sawParseWarn agent.Event
			for _, e := range events.events {
				if e.Kind == agent.EventWritebackError {
					sawParseWarn = e
					break
				}
			}
			require.NotZero(t, sawParseWarn, "EventWritebackError captures the parse miss")
			assert.Contains(t, sawParseWarn.Error, tt.wantWarnSubstr,
				"EventWritebackError carries the parse failure message")
			assert.Contains(t, sawParseWarn.Error, agent.ErrMalformedWriteback.Error())

			// The turn still ends with turn_finished — never turn_failed.
			for _, e := range events.events {
				assert.NotEqual(t, agent.EventTurnFailed, e.Kind,
					"the turn still ends with turn_finished, not turn_failed")
			}
			terminal := events.events[len(events.events)-1]
			assert.Equal(t, agent.EventTurnFinished, terminal.Kind)
			assert.Equal(t, "still useful", terminal.Response)
		})
	}
}

// TestSessionRun_StrictWritebackSchema is the contract-strict sibling
// to TestSessionRun_LenientWritebackParse: a writeback whose JSON parses
// but violates the operation schema still fails the turn, so the model
// learns the contract (issue #93).
func TestSessionRun_StrictWritebackSchema(t *testing.T) {
	model := agent.NewScriptedModel(
		agent.ScriptedStep{Content: "broken\n```memory\n{\"keep\":[\"\"]}\n```"},
	)
	events := &fakeEvents{}
	s := agent.NewSession(model, tool.NewRegistry(), agent.Options{Events: events})

	_, err := s.Run(context.Background(), "task")
	require.Error(t, err)
	assert.True(t, errors.Is(err, agent.ErrMalformedWriteback))

	terminal := events.events[len(events.events)-1]
	assert.Equal(t, agent.EventTurnFailed, terminal.Kind,
		"schema violations still terminate the turn")
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

// countingTool always succeeds and records how often it was executed.
type countingTool struct {
	name  string
	calls int
}

func (t *countingTool) Name() string                  { return t.name }
func (t *countingTool) Description() string           { return "counts executions" }
func (t *countingTool) InputSchema() json.RawMessage  { return nil }
func (t *countingTool) OutputSchema() json.RawMessage { return nil }

func (t *countingTool) Call(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
	t.calls++
	return json.RawMessage(`{"ok":true}`), nil
}

// searchSteps scripts one successful search call per model response, with a
// varying query so the identical-call guard never matches (issue #160).
func searchSteps(queries ...string) []agent.ScriptedStep {
	steps := make([]agent.ScriptedStep, 0, len(queries))
	for _, q := range queries {
		input, _ := json.Marshal(map[string]string{"query": q})
		steps = append(steps, agent.ScriptedStep{
			Content:   "searching\n```memory\n{}\n```",
			ToolCalls: []agent.ToolCall{{Name: "search", Input: input}},
		})
	}
	return steps
}

func TestSessionRun_ToolBudgetShortCircuits(t *testing.T) {
	search := &countingTool{name: "search"}
	events := &fakeEvents{}
	steps := append(searchSteps("q1", "q2", "q3", "q4", "q5", "q6", "q7", "q8"),
		agent.ScriptedStep{Content: "done\n```memory\n{}\n```"})
	s := agent.NewSession(agent.NewScriptedModel(steps...), registryWith(t, search), agent.Options{Events: events})

	final, err := s.Run(context.Background(), "task")
	require.NoError(t, err)
	assert.Equal(t, "done", final, "the turn reaches a final response instead of exhausting the loop")
	assert.Equal(t, 6, search.calls, "calls past the per-tool budget are not executed")

	// The blocked calls are fed back as failed tool results and stay
	// visible on the monitoring stream.
	var toolEvents []agent.Event
	for _, e := range events.events {
		if e.Kind == agent.EventToolCall {
			toolEvents = append(toolEvents, e)
		}
	}
	require.Len(t, toolEvents, 8)
	for _, e := range toolEvents[:6] {
		assert.False(t, e.ToolFailed, "calls within the budget execute")
	}
	for _, e := range toolEvents[6:] {
		assert.True(t, e.ToolFailed)
		assert.Contains(t, e.ToolResult, "Not executed")
		assert.Contains(t, e.ToolResult, `exhausted for this turn`)
		assert.Contains(t, e.ToolResult, "(tool description: counts executions)",
			"the notice restates the tool's contract so an exhausted model is steered at the decision point (issue #164)")
	}
}

// TestSessionRun_ToolBudgetNoticeOmitsEmptyDescription pins the fallback: a
// tool without a description (here an unregistered name, which the budget
// still counts) keeps the bare exhaustion notice.
func TestSessionRun_ToolBudgetNoticeOmitsEmptyDescription(t *testing.T) {
	events := &fakeEvents{}
	steps := append(searchSteps("q1", "q2", "q3", "q4", "q5", "q6", "q7"),
		agent.ScriptedStep{Content: "done\n```memory\n{}\n```"})
	s := agent.NewSession(agent.NewScriptedModel(steps...), tool.NewRegistry(), agent.Options{Events: events})

	_, err := s.Run(context.Background(), "task")
	require.NoError(t, err)

	var last *agent.Event
	for i := range events.events {
		if events.events[i].Kind == agent.EventToolCall {
			last = &events.events[i]
		}
	}
	require.NotNil(t, last)
	assert.True(t, last.ToolFailed)
	assert.Contains(t, last.ToolResult, "exhausted for this turn")
	assert.NotContains(t, last.ToolResult, "tool description",
		"a tool without a description keeps the bare budget notice")
}

func TestSessionRun_ToolBudgetIsPerTool(t *testing.T) {
	search := &countingTool{name: "search"}
	files := &countingTool{name: "files"}
	steps := append(searchSteps("q1", "q2", "q3", "q4", "q5", "q6", "q7"),
		agent.ScriptedStep{
			Content:   "reading\n```memory\n{}\n```",
			ToolCalls: []agent.ToolCall{{Name: "files", Input: json.RawMessage(`{"op":"read"}`)}},
		},
		agent.ScriptedStep{Content: "done\n```memory\n{}\n```"})
	s := agent.NewSession(agent.NewScriptedModel(steps...), registryWith(t, search, files), agent.Options{})

	final, err := s.Run(context.Background(), "task")
	require.NoError(t, err)
	assert.Equal(t, "done", final)
	assert.Equal(t, 6, search.calls, "the search budget is spent")
	assert.Equal(t, 1, files.calls, "a different tool is unaffected by the exhausted budget")
}

func TestSessionRun_ToolBudgetResetsPerTurn(t *testing.T) {
	search := &countingTool{name: "search"}
	steps := append(searchSteps("q1", "q2", "q3", "q4", "q5", "q6", "q7"),
		agent.ScriptedStep{Content: "one\n```memory\n{}\n```"})
	steps = append(steps, searchSteps("r1", "r2", "r3", "r4", "r5", "r6")...)
	steps = append(steps, agent.ScriptedStep{Content: "two\n```memory\n{}\n```"})
	s := agent.NewSession(agent.NewScriptedModel(steps...), registryWith(t, search), agent.Options{})

	_, err := s.Run(context.Background(), "task one")
	require.NoError(t, err)
	require.Equal(t, 6, search.calls)

	_, err = s.Run(context.Background(), "task two")
	require.NoError(t, err)
	assert.Equal(t, 12, search.calls, "the second turn starts with a fresh per-tool budget")
}

// TestSessionRun_RepeatedGuardPrecedesBudget is issue #160's companion to
// TestSessionRun_ToolBudgetShortCircuits: within the budget, the
// identical-call guard still fires with its own notice; the budget notice
// only appears once the call count passes it.
func TestSessionRun_RepeatedGuardPrecedesBudget(t *testing.T) {
	flaky := &failingTool{errs: []string{"boom"}}
	call := agent.ToolCall{Name: "flaky", Input: json.RawMessage(`{"n":1}`)}
	steps := make([]agent.ScriptedStep, 0, 9)
	for range 8 {
		steps = append(steps, agent.ScriptedStep{
			Content:   "retrying\n```memory\n{}\n```",
			ToolCalls: []agent.ToolCall{call},
		})
	}
	steps = append(steps, agent.ScriptedStep{Content: "done\n```memory\n{}\n```"})
	s := agent.NewSession(agent.NewScriptedModel(steps...), registryWith(t, flaky), agent.Options{})

	final, err := s.Run(context.Background(), "task")
	require.NoError(t, err)
	assert.Equal(t, "done", final)
	assert.Equal(t, 2, flaky.calls, "the identical failing call is executed twice, then short-circuited")

	var notices []string
	for _, msg := range s.History() {
		if msg.Role == agent.RoleTool {
			notices = append(notices, msg.Content)
		}
	}
	require.Len(t, notices, 8)
	assert.Contains(t, notices[2], "you already made this exact call",
		"the third identical call hits the repeat guard, not the budget")
	assert.Contains(t, notices[5], "you already made this exact call",
		"the repeat guard still fires at the budget boundary")
	assert.Contains(t, notices[6], "exhausted for this turn",
		"the budget notice appears only past the budget")
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
