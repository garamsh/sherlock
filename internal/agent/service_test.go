package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	agentmemory "github.com/garamsh/sherlock/internal/agent/memory"
	"github.com/garamsh/sherlock/internal/tool"
)

// replyOnly renders the response document (agent.ResponseSchema).
func replyOnly(text string) string {
	out, err := json.Marshal(struct {
		Reply string `json:"reply"`
	}{Reply: text})
	if err != nil {
		panic(err)
	}
	return string(out)
}

// finalStep scripts the call a turn ends with: the reserved writeback
// tool carrying the response document. A turn no longer ends by
// returning content, so every terminal step in these tests is one of
// these (issue #615).
func finalStep(text string) agent.ScriptedStep {
	return agent.ScriptedStep{ToolCalls: []agent.ToolCall{writebackCall(replyOnly(text))}}
}

// writebackCall is one reserved writeback call carrying a document.
func writebackCall(document string) agent.ToolCall {
	return agent.ToolCall{Name: agent.MemoryWriteTool, Input: json.RawMessage(document)}
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

// fakeClient is the test stand-in for the platform tool client. It exposes
// a descriptor list and a single call handler; the descriptor is sourced
// from the registered tool, the call routes to the matching handler.
type fakeClient struct {
	mu        sync.Mutex
	tools     map[string]*fakeTool
	errByName map[string]error
}

func newFakeClient() *fakeClient {
	return &fakeClient{
		tools:     map[string]*fakeTool{},
		errByName: map[string]error{},
	}
}

func (f *fakeClient) register(t *fakeTool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tools[t.name] = t
}

func (f *fakeClient) setErr(name string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errByName[name] = err
}

func (f *fakeClient) List() []tool.Descriptor {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]tool.Descriptor, 0, len(f.tools))
	for _, t := range f.tools {
		out = append(out, tool.Descriptor{
			Name:         t.name,
			Description:  t.description,
			InputSchema:  t.inputSchema,
			OutputSchema: t.outputSchema,
		})
	}
	return out
}

func (f *fakeClient) Call(_ context.Context, name string, _ json.RawMessage) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tools[name]
	if !ok {
		return nil, errors.New("tool not found: " + name)
	}
	t.calls++
	if err, ok := f.errByName[name]; ok {
		return nil, err
	}
	return t.response, nil
}

// fakeTool is the test stand-in for one registered tool description.
type fakeTool struct {
	name         string
	description  string
	inputSchema  json.RawMessage
	outputSchema json.RawMessage
	response     json.RawMessage
	calls        int
}

// newEchoTool returns a fake tool that echoes its input text.
func newEchoTool() *fakeTool {
	return &fakeTool{
		name:         "echo",
		description:  "echo the text back",
		inputSchema:  nil,
		outputSchema: nil,
		response:     json.RawMessage(`{"echo":"hi"}`),
	}
}

func newFakeClientWith(t *fakeTool) *fakeClient {
	c := newFakeClient()
	c.register(t)
	return c
}

func TestSessionRun_ScriptedMultiTurnScenario(t *testing.T) {
	// Script: the model requests the echo tool, then ends the turn on the
	// reserved writeback. A turn writes once, when it ends, rather than
	// once per model response.
	model := agent.NewScriptedModel(
		agent.ScriptedStep{
			Content: "Let me check.",
			ToolCalls: []agent.ToolCall{
				{Name: "echo", Input: json.RawMessage(`{"text":"hi"}`)},
			},
		},
		agent.ScriptedStep{
			ToolCalls: []agent.ToolCall{writebackCall(`{"reply":"The echo said hi."}`)},
		},
	)
	events := &fakeEvents{}

	s := agent.NewSession(model, newFakeClientWith(newEchoTool()), agent.Options{
		Events: events,
	})

	final, err := s.Run(context.Background(), "what did the echo say?")
	require.NoError(t, err)
	assert.Equal(t, "The echo said hi.", final)

	// The task follows the injected instructions directly: nothing is
	// recalled before the act loop.
	history := s.History()
	require.Len(t, history, 5)
	assert.Equal(t, agent.RoleSystem, history[0].Role)
	assert.Equal(t, agent.ComposeInstructions("", ""), history[0].Content)
	assert.Equal(t, agent.RoleUser, history[1].Role)
	assert.Equal(t, "what did the echo say?", history[1].Content)
	assert.Equal(t, agent.RoleAssistant, history[2].Role)
	require.Len(t, history[2].ToolCalls, 1)
	assert.Equal(t, "echo", history[2].ToolCalls[0].Name)
	assert.NotEmpty(t, history[2].ToolCalls[0].ID, "scripted model assigns tool-call IDs")

	// The tool result was incorporated before the final response.
	assert.Equal(t, agent.RoleTool, history[3].Role)
	assert.Equal(t, `{"echo":"hi"}`, history[3].Content)
	assert.Equal(t, history[2].ToolCalls[0].ID, history[3].ToolCallID)
	assert.Equal(t, agent.RoleAssistant, history[4].Role)

	// Events fired in order within the turn.
	require.Equal(t, []agent.EventKind{
		agent.EventTurnStarted,
		agent.EventToolCall,
		agent.EventWriteback,
		agent.EventTurnFinished,
	}, events.kinds())
	assert.Equal(t, "what did the echo say?", events.events[0].Task)
	toolEvent := events.events[1]
	assert.Equal(t, "echo", toolEvent.ToolName)
	assert.JSONEq(t, `{"text":"hi"}`, string(toolEvent.ToolInput))
	assert.Equal(t, `{"echo":"hi"}`, toolEvent.ToolResult)
	assert.False(t, toolEvent.ToolFailed)
	assert.Equal(t, "The echo said hi.", events.events[3].Response)
	for _, e := range events.events {
		assert.Equal(t, 1, e.Turn)
	}
}

func TestSessionRun_RollingSession(t *testing.T) {
	model := agent.NewScriptedModel(
		finalStep("first"),
		finalStep("second"),
	)
	events := &fakeEvents{}
	s := agent.NewSession(model, newFakeClient(), agent.Options{Events: events})

	first, err := s.Run(context.Background(), "task one")
	require.NoError(t, err)
	second, err := s.Run(context.Background(), "task two")
	require.NoError(t, err)
	assert.Equal(t, "first", first)
	assert.Equal(t, "second", second)

	// One rolling session: history accumulates, turn numbers increment.
	assert.Len(t, s.History(), 5)
	assert.Equal(t, 1, events.events[0].Turn)
	last := events.events[len(events.events)-1]
	assert.Equal(t, agent.EventTurnFinished, last.Kind)
	assert.Equal(t, 2, last.Turn)
}

// TestSessionRun_ModelSeesTheReservedWritebackTool pins the half of the
// design that decides whether the writeback is declinable. The model is
// shown the tool — it must be, to call it — but the tool is the
// platform's: it is added to what the model sees rather than installed
// by the operator, and an operator tool taking the reserved name is
// dropped rather than silently shadowing it.
func TestSessionRun_ModelSeesTheReservedWritebackTool(t *testing.T) {
	client := newFakeClientWith(newEchoTool())
	client.register(&fakeTool{name: agent.MemoryWriteTool, description: "an impostor"})
	model := &toolCapturingModel{}
	s := agent.NewSession(model, client, agent.Options{})

	_, err := s.Run(context.Background(), "task")
	require.NoError(t, err)

	require.NotEmpty(t, model.seen)
	var names []string
	var reserved int
	for _, d := range model.seen[0] {
		names = append(names, d.Name)
		if d.Name == agent.MemoryWriteTool {
			reserved++
			assert.Equal(t, agent.MemoryWriteDescriptor().Description, d.Description,
				"the reserved descriptor is the platform's, not the operator's")
		}
	}
	assert.Contains(t, names, "echo", "the operator's tools still reach the model")
	assert.Equal(t, 1, reserved, "the reserved name appears exactly once")

	// It is not reachable through the tool client: the session ends the
	// turn on it instead of dispatching it.
	assert.Zero(t, client.tools[agent.MemoryWriteTool].calls)
}

// toolCapturingModel records the descriptors it was shown and ends the
// turn the way a turn ends.
type toolCapturingModel struct {
	seen [][]tool.Descriptor
}

func (m *toolCapturingModel) Chat(_ context.Context, _ []agent.Message, tools []tool.Descriptor) (agent.Response, error) {
	m.seen = append(m.seen, append([]tool.Descriptor(nil), tools...))
	return agent.Response{ToolCalls: []agent.ToolCall{writebackCall(replyOnly("done"))}}, nil
}

// TestSessionRun_HistoryLeavesNoUnansweredToolCall pins what a rolling
// session hands the provider on its next turn. The writeback call is a
// turn's last act and nothing answers it, so keeping it in history
// leaves a tool call with no result — which MiniMax rejects with HTTP
// 400 "tool call result does not follow tool call", making a session
// unusable past its first turn (#630). History keeps the reply instead.
func TestSessionRun_HistoryLeavesNoUnansweredToolCall(t *testing.T) {
	model := agent.NewScriptedModel(
		agent.ScriptedStep{
			Content:   "checking",
			ToolCalls: []agent.ToolCall{{Name: "echo", Input: json.RawMessage(`{"text":"hi"}`)}},
		},
		finalStep("done"),
	)
	s := agent.NewSession(model, newFakeClientWith(newEchoTool()), agent.Options{})

	_, err := s.Run(context.Background(), "task")
	require.NoError(t, err)

	history := s.History()
	answered := map[string]bool{}
	for _, m := range history {
		if m.Role == agent.RoleTool {
			answered[m.ToolCallID] = true
		}
	}
	for i, m := range history {
		for _, call := range m.ToolCalls {
			assert.True(t, answered[call.ID],
				"history[%d] requests %q and nothing answers it", i, call.Name)
		}
	}
	assert.Equal(t, "done", history[len(history)-1].Content,
		"the turn's last message is the reply, not the call that carried it")
}

// TestSessionRun_ProseInsteadOfAWritebackDoesNotEndTheTurn is issue
// #615's regression, and it is the shape a provider that ignores what it
// was sent produces: content, no tool calls, HTTP 200. Under the
// response format this parsed to no operations and the turn finished, so
// the user got a reply and memory got nothing — the 0.00 write path
// #336 step 1 removed. The turn is not ended on it now: the session says
// what is missing and asks again.
func TestSessionRun_ProseInsteadOfAWritebackDoesNotEndTheTurn(t *testing.T) {
	model := agent.NewScriptedModel(
		agent.ScriptedStep{Content: "<think>The user says hello.</think> Hello!"},
		finalStep("Hello!"),
	)
	events := &fakeEvents{}
	s := agent.NewSession(model, newFakeClient(), agent.Options{Events: events})

	final, err := s.Run(context.Background(), "hello")
	require.NoError(t, err, "the model complied on the second call")
	assert.Equal(t, "Hello!", final)

	// The turn wrote, because it ended the only way a turn can.
	assert.Contains(t, events.kinds(), agent.EventWriteback)

	// The refusal is on the stream, so a provider that does this is
	// visible rather than silent.
	var sawRefusal bool
	for _, e := range events.events {
		if e.Kind == agent.EventWritebackError {
			sawRefusal = true
			assert.Contains(t, e.Error, agent.MemoryWriteTool)
		}
	}
	assert.True(t, sawRefusal, "the dropped turn-ending attempt reaches the event stream")

	// The model was told what was missing.
	var told bool
	for _, m := range s.History() {
		if m.Role == agent.RoleSystem && strings.Contains(m.Content, agent.MemoryWriteTool) {
			told = true
		}
	}
	assert.True(t, told, "the session names the call the turn needs")
}

// TestSessionRun_ProseForeverFailsTheTurn is the other half: a provider
// that never honours the tool exhausts the loop and fails. A turn that
// cannot write must not look like a turn that stated no facts, which is
// what made the MiniMax defect silent (issue #615).
func TestSessionRun_ProseForeverFailsTheTurn(t *testing.T) {
	steps := make([]agent.ScriptedStep, 4)
	for i := range steps {
		steps[i] = agent.ScriptedStep{Content: "prose, no call"}
	}
	events := &fakeEvents{}
	s := agent.NewSession(agent.NewScriptedModel(steps...), newFakeClient(), agent.Options{
		Events:        events,
		MaxIterations: 4,
	})

	final, err := s.Run(context.Background(), "task")
	require.Error(t, err, "a turn that never writes fails rather than replying")
	assert.True(t, errors.Is(err, agent.ErrLoopExhausted))
	assert.Empty(t, final)
	assert.NotContains(t, events.kinds(), agent.EventWriteback)

	terminal := events.events[len(events.events)-1]
	assert.Equal(t, agent.EventTurnFailed, terminal.Kind)
}

// TestSessionRun_MalformedWritebackArgumentsFailTheTurn pins where the
// leniency issue #93 asked for now sits. The arguments are the only
// place the reply arrives, so arguments that are not the document leave
// nothing to hand back: the turn fails rather than returning a reply
// that was never written down.
func TestSessionRun_MalformedWritebackArgumentsFailTheTurn(t *testing.T) {
	model := agent.NewScriptedModel(
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{writebackCall(`{"reply":"still useful","note":`)}},
	)
	events := &fakeEvents{}
	s := agent.NewSession(model, newFakeClient(), agent.Options{Events: events})

	_, err := s.Run(context.Background(), "task")
	require.Error(t, err)
	assert.True(t, errors.Is(err, agent.ErrMalformedWriteback))
	assert.NotContains(t, events.kinds(), agent.EventWriteback)

	terminal := events.events[len(events.events)-1]
	assert.Equal(t, agent.EventTurnFailed, terminal.Kind)
}

func TestSessionRun_UnknownToolFeedsErrorBack(t *testing.T) {
	model := agent.NewScriptedModel(
		agent.ScriptedStep{
			Content:   "calling",
			ToolCalls: []agent.ToolCall{{Name: "ghost", Input: json.RawMessage(`{}`)}},
		},
		finalStep("recovered"),
	)
	events := &fakeEvents{}
	s := agent.NewSession(model, newFakeClient(), agent.Options{Events: events})

	final, err := s.Run(context.Background(), "task")
	require.NoError(t, err)
	assert.Equal(t, "recovered", final)

	toolMsg := s.History()[3]
	assert.Equal(t, agent.RoleTool, toolMsg.Role)
	assert.Contains(t, toolMsg.Content, "tool not found")
	// turn_started, then the tool call: the writeback event fires once
	// per turn now, when the turn ends on it, rather than once per
	// model response.
	toolEvent := events.events[1]
	assert.Equal(t, agent.EventToolCall, toolEvent.Kind)
	assert.True(t, toolEvent.ToolFailed)
	assert.Contains(t, toolEvent.ToolResult, "tool not found")
}

func TestSessionRun_ScriptExhausted(t *testing.T) {
	model := agent.NewScriptedModel(
		agent.ScriptedStep{
			Content:   "loop",
			ToolCalls: []agent.ToolCall{{Name: "ghost", Input: json.RawMessage(`{}`)}},
		},
	)
	s := agent.NewSession(model, newFakeClient(), agent.Options{})

	_, err := s.Run(context.Background(), "task")
	require.Error(t, err)
	assert.True(t, errors.Is(err, agent.ErrScriptExhausted))
}

func TestSessionRun_LoopExhausted(t *testing.T) {
	model := agent.NewScriptedModel(
		agent.ScriptedStep{
			Content:   "again",
			ToolCalls: []agent.ToolCall{{Name: "ghost", Input: json.RawMessage(`{}`)}},
		},
		agent.ScriptedStep{
			Content:   "again",
			ToolCalls: []agent.ToolCall{{Name: "ghost", Input: json.RawMessage(`{}`)}},
		},
	)
	s := agent.NewSession(model, newFakeClient(), agent.Options{MaxIterations: 2})

	_, err := s.Run(context.Background(), "task")
	require.Error(t, err)
	assert.True(t, errors.Is(err, agent.ErrLoopExhausted))
}

// stubbornModel re-issues one failing tool call until a tool result tells it
// the call was not executed, then answers.
type stubbornModel struct {
	call agent.ToolCall
}

func (m *stubbornModel) Chat(_ context.Context, messages []agent.Message, _ []tool.Descriptor) (agent.Response, error) {
	last := messages[len(messages)-1]
	if last.Role == agent.RoleTool && strings.Contains(last.Content, "Not executed") {
		return agent.Response{ToolCalls: []agent.ToolCall{writebackCall(replyOnly("changing approach"))}}, nil
	}
	return agent.Response{
		Content:   "retrying",
		ToolCalls: []agent.ToolCall{m.call},
	}, nil
}

func TestSessionRun_RepeatedFailingCallShortCircuits(t *testing.T) {
	flaky := &fakeTool{
		name: "flaky",
	}
	client := newFakeClient()
	client.register(flaky)
	client.setErr(flaky.name, errors.New("boom"))
	model := &stubbornModel{call: agent.ToolCall{Name: "flaky", Input: json.RawMessage(`{"n":1}`)}}
	events := &fakeEvents{}
	s := agent.NewSession(model, client, agent.Options{Events: events})

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
	steps := []agent.ScriptedStep{
		{Content: "calling", ToolCalls: []agent.ToolCall{{Name: "flaky", Input: json.RawMessage(`{"n":1}`)}}},
		{Content: "calling", ToolCalls: []agent.ToolCall{{Name: "flaky", Input: json.RawMessage(`{"n":1}`)}}},
		{Content: "calling", ToolCalls: []agent.ToolCall{{Name: "flaky", Input: json.RawMessage(`{"n":2}`)}}},
		finalStep("done"),
	}
	flaky := &fakeTool{name: "flaky", response: json.RawMessage(`{}`)}
	client := newFakeClient()
	client.register(flaky)
	// The first two calls fail identically; the third call (different
	// input) executes. The session must not short-circuit the third
	// call on the repeat guard.
	client.setErr("flaky", errors.New("boom"))
	s := agent.NewSession(agent.NewScriptedModel(steps...), client, agent.Options{})

	final, err := s.Run(context.Background(), "task")
	require.NoError(t, err)
	assert.Equal(t, "done", final)
	assert.Equal(t, 3, flaky.calls, "calls with different inputs are not short-circuited")
	for _, msg := range s.History() {
		assert.NotContains(t, msg.Content, "Not executed", "no call was short-circuited")
	}
}

func TestSessionRun_RepeatBookkeepingResetsPerTurn(t *testing.T) {
	flaky := &fakeTool{name: "flaky"}
	client := newFakeClient()
	client.register(flaky)
	client.setErr(flaky.name, errors.New("boom"))
	call := agent.ToolCall{Name: "flaky", Input: json.RawMessage(`{"n":1}`)}
	step := agent.ScriptedStep{Content: "calling", ToolCalls: []agent.ToolCall{call}}
	final := finalStep("done")
	s := agent.NewSession(agent.NewScriptedModel(step, step, final, step, step, final), client, agent.Options{})

	_, err := s.Run(context.Background(), "task one")
	require.NoError(t, err)
	require.Equal(t, 2, flaky.calls)

	_, err = s.Run(context.Background(), "task two")
	require.NoError(t, err)
	assert.Equal(t, 4, flaky.calls, "the second turn starts with empty repeat bookkeeping")
}

// searchSteps scripts one successful search call per model response, with a
// varying query so the identical-call guard never matches (issue #160).
func searchSteps(queries ...string) []agent.ScriptedStep {
	steps := make([]agent.ScriptedStep, 0, len(queries))
	for _, q := range queries {
		input, _ := json.Marshal(map[string]string{"query": q})
		steps = append(steps, agent.ScriptedStep{
			Content:   "searching",
			ToolCalls: []agent.ToolCall{{Name: "search", Input: input}},
		})
	}
	return steps
}

func TestSessionRun_ToolBudgetShortCircuits(t *testing.T) {
	search := &fakeTool{name: "search", description: "counts executions", response: json.RawMessage(`{"ok":true}`)}
	events := &fakeEvents{}
	steps := append(searchSteps("q1", "q2", "q3", "q4", "q5", "q6", "q7", "q8"),
		finalStep("done"))
	s := agent.NewSession(agent.NewScriptedModel(steps...), newFakeClientWith(search), agent.Options{Events: events})

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
		finalStep("done"))
	s := agent.NewSession(agent.NewScriptedModel(steps...), newFakeClient(), agent.Options{Events: events})

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
	search := &fakeTool{name: "search", description: "counts executions", response: json.RawMessage(`{"ok":true}`)}
	files := &fakeTool{name: "files", description: "files", response: json.RawMessage(`{"ok":true}`)}
	steps := append(searchSteps("q1", "q2", "q3", "q4", "q5", "q6", "q7"),
		agent.ScriptedStep{
			Content:   "reading",
			ToolCalls: []agent.ToolCall{{Name: "files", Input: json.RawMessage(`{"op":"read"}`)}},
		},
		finalStep("done"))
	client := newFakeClient()
	client.register(search)
	client.register(files)
	s := agent.NewSession(agent.NewScriptedModel(steps...), client, agent.Options{})

	final, err := s.Run(context.Background(), "task")
	require.NoError(t, err)
	assert.Equal(t, "done", final)
	assert.Equal(t, 6, search.calls, "the search budget is spent")
	assert.Equal(t, 1, files.calls, "a different tool is unaffected by the exhausted budget")
}

func TestSessionRun_ToolBudgetResetsPerTurn(t *testing.T) {
	search := &fakeTool{name: "search", response: json.RawMessage(`{"ok":true}`)}
	steps := append(searchSteps("q1", "q2", "q3", "q4", "q5", "q6", "q7"),
		finalStep("one"))
	steps = append(steps, searchSteps("r1", "r2", "r3", "r4", "r5", "r6")...)
	steps = append(steps, finalStep("two"))
	s := agent.NewSession(agent.NewScriptedModel(steps...), newFakeClientWith(search), agent.Options{})

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
	flaky := &fakeTool{name: "flaky"}
	client := newFakeClient()
	client.register(flaky)
	client.setErr(flaky.name, errors.New("boom"))
	call := agent.ToolCall{Name: "flaky", Input: json.RawMessage(`{"n":1}`)}
	steps := make([]agent.ScriptedStep, 0, 9)
	for range 8 {
		steps = append(steps, agent.ScriptedStep{
			Content:   "retrying",
			ToolCalls: []agent.ToolCall{call},
		})
	}
	steps = append(steps, finalStep("done"))
	s := agent.NewSession(agent.NewScriptedModel(steps...), client, agent.Options{})

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
		finalStep("one"),
		finalStep("two"),
	)
	compactor := &fakeCompactor{}
	s := agent.NewSession(model, newFakeClient(), agent.Options{
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
	s := agent.NewSession(model, newFakeClient(), agent.Options{})

	_, err := s.Run(context.Background(), "task")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider down")
}

func TestSessionRun_FailureEmitsTerminalEvent(t *testing.T) {
	model := agent.NewScriptedModel(
		agent.ScriptedStep{Err: errors.New("provider down")},
	)
	events := &fakeEvents{}
	s := agent.NewSession(model, newFakeClient(), agent.Options{Events: events})

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
	return agent.Response{ToolCalls: []agent.ToolCall{writebackCall(replyOnly("done"))}}, nil
}

func TestSessionRun_InstructionsAreFirstSystemMessage(t *testing.T) {
	model := &captureModel{}
	s := agent.NewSession(model, newFakeClient(), agent.Options{})

	_, err := s.Run(context.Background(), "task")
	require.NoError(t, err)

	messages := model.calls[0]
	require.Len(t, messages, 2)
	assert.Equal(t, agent.RoleSystem, messages[0].Role)
	assert.Equal(t, agent.ComposeInstructions("", ""), messages[0].Content)
	assert.Equal(t, agent.RoleUser, messages[1].Role, "the task follows the instructions")
}

func TestSessionRun_CustomEgoKeepsContract(t *testing.T) {
	model := &captureModel{}
	s := agent.NewSession(model, newFakeClient(), agent.Options{Ego: "You are a test ego."})

	_, err := s.Run(context.Background(), "task")
	require.NoError(t, err)

	instructions := model.calls[0][0].Content
	assert.True(t, strings.HasPrefix(instructions, "You are a test ego."), "ego leads the instructions")
	assert.Contains(t, instructions, agent.Contract, "platform contract survives a custom ego")
}

func TestSessionRun_OperatorInstructionsReachTheModel(t *testing.T) {
	model := &captureModel{}
	s := agent.NewSession(model, newFakeClient(), agent.Options{OperatorInstructions: "Answer every envelope."})

	_, err := s.Run(context.Background(), "task")
	require.NoError(t, err)

	assert.Equal(t, agent.ComposeInstructions("", "Answer every envelope."), model.calls[0][0].Content)
	assert.Contains(t, model.calls[0][0].Content, "Answer every envelope.")
}

func TestSessionRun_InstructionsInjectedOnce(t *testing.T) {
	model := &captureModel{}
	s := agent.NewSession(model, newFakeClient(), agent.Options{})

	_, err := s.Run(context.Background(), "task one")
	require.NoError(t, err)
	_, err = s.Run(context.Background(), "task two")
	require.NoError(t, err)

	second := model.calls[1]
	var injections int
	for _, msg := range second {
		if msg.Content == agent.ComposeInstructions("", "") {
			injections++
		}
	}
	assert.Equal(t, 1, injections, "instructions are injected once, on the first turn")
	require.Len(t, model.calls, 2)
}

// TestSessionRun_InstructionsSurviveCompaction crosses the boundary #642
// fell at: a session whose history overflows keeps its instructions at
// the head, so the model is still answering to the ego and contract —
// and still ending turns with the writeback call they instruct — after
// the first compaction.
func TestSessionRun_InstructionsSurviveCompaction(t *testing.T) {
	model := &captureModel{}
	s := agent.NewSession(model, newFakeClient(), agent.Options{
		Compactor:          agentmemory.Compactor{MaxHistoryMessages: 3},
		MaxHistoryMessages: 3,
	})

	_, err := s.Run(context.Background(), "task one")
	require.NoError(t, err)
	_, err = s.Run(context.Background(), "task two")
	require.NoError(t, err)

	last := model.calls[len(model.calls)-1]
	require.NotEmpty(t, last)
	assert.Equal(t, agent.RoleSystem, last[0].Role)
	assert.Equal(t, agent.ComposeInstructions("", ""), last[0].Content,
		"the instructions still lead the history the model sees after a compaction")
}

func TestSessionRun_MemoryFilesLeadEveryTurn(t *testing.T) {
	store := &fileStore{}
	model := &recordingModel{inner: agent.NewScriptedModel(
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: agent.MemoryFileWriteTool, Input: json.RawMessage(`{"file":"USER.md","content":" Indent code with tabs. "}`)}}},
		finalStep("Noted."),
		finalStep("Here it is."),
	)}
	s := agent.NewSession(model, newFakeClient(), agent.Options{Conversation: store})

	_, err := s.Run(context.Background(), "Always indent code with tabs.")
	require.NoError(t, err)
	_, err = s.Run(context.Background(), "Write me a loop.")
	require.NoError(t, err)

	empty := "\n\n## Your memory files\n### USER.md\n(empty)\n### MEMORY.md\n(empty)\n\n## Your skills\n(none)"
	saved := "\n\n## Your memory files\n### USER.md\nIndent code with tabs.\n### MEMORY.md\n(empty)\n\n## Your skills\n(none)"
	assert.Equal(t, agent.ComposeInstructions("", "")+empty, model.seen[0][0].Content, "unwritten files are stated as empty")
	assert.Equal(t, agent.ComposeInstructions("", "")+saved, model.seen[2][0].Content, "the next turn leads with the saved file")
	assert.Equal(t, []string{"USER.md"}, store.writes, "only the call writes a file; ending the turn writes none")
	require.Len(t, store.turns, 2, "sources are recorded on both turns")
}

// TestSessionRun_OvergrownMemoryFileIsRefused hands a write past the file's
// bound back to the model as a failed call and keeps the saved file; a
// write at the bound, the control, is saved.
func TestSessionRun_OvergrownMemoryFileIsRefused(t *testing.T) {
	at := strings.Repeat("é", 1375)
	store := &fileStore{}
	model := &recordingModel{inner: agent.NewScriptedModel(
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: agent.MemoryFileWriteTool, Input: json.RawMessage(`{"file":"USER.md","content":"` + at + `x"}`)}}},
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: agent.MemoryFileWriteTool, Input: json.RawMessage(`{"file":"USER.md","content":"` + at + `"}`)}}},
		finalStep("Done."),
	)}
	events := &fakeEvents{}
	s := agent.NewSession(model, newFakeClient(), agent.Options{Conversation: store, Events: events})

	reply, err := s.Run(context.Background(), "task")
	require.NoError(t, err)
	assert.Equal(t, "Done.", reply)
	assert.Equal(t, map[string]string{"USER.md": at}, store.files, "only the write within the bound is saved")

	var calls []agent.Event
	for _, e := range events.events {
		if e.Kind == agent.EventToolCall {
			calls = append(calls, e)
		}
	}
	require.Len(t, calls, 2)
	assert.True(t, calls[0].ToolFailed)
	assert.Contains(t, calls[0].ToolResult, "USER.md is 1376 characters, at most 1375")
	assert.False(t, calls[1].ToolFailed)
}

// TestSessionRun_SkillsAreIndexedAndLoadedOnDemand shows a written skill's
// description in every later turn's instructions and its body only in the
// result of the call that loads it.
func TestSessionRun_SkillsAreIndexedAndLoadedOnDemand(t *testing.T) {
	store := &fileStore{}
	model := &recordingModel{inner: agent.NewScriptedModel(
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: agent.SkillWriteTool, Input: json.RawMessage(`{"name":"python-module","description":"How the person wants a Python module written.","body":"Start the file with the SPDX line."}`)}}},
		finalStep("Saved."),
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: agent.SkillViewTool, Input: json.RawMessage(`{"name":"python-module"}`)}}},
		finalStep("Written."),
	)}
	events := &fakeEvents{}
	s := agent.NewSession(model, newFakeClient(), agent.Options{Conversation: store, Events: events})

	_, err := s.Run(context.Background(), "Remember how I want modules written.")
	require.NoError(t, err)
	_, err = s.Run(context.Background(), "Write a module.")
	require.NoError(t, err)

	lead := model.seen[2][0].Content
	assert.Contains(t, lead, "## Your skills\n- python-module: How the person wants a Python module written.")
	assert.NotContains(t, lead, "Start the file with the SPDX line.", "the body is not in context until loaded")
	var loaded string
	for _, e := range events.events {
		if e.Kind == agent.EventToolCall && e.ToolName == agent.SkillViewTool {
			loaded = e.ToolResult
		}
	}
	assert.Equal(t, "Start the file with the SPDX line.", loaded)
}

// TestSessionRun_SkillWriteRefusals refuses a name that is not kebab-case
// and a seventeenth skill, and saves a well-formed one as the control.
func TestSessionRun_SkillWriteRefusals(t *testing.T) {
	full := &fileStore{files: map[string]string{}}
	for i := range 16 {
		full.files[fmt.Sprintf("skills/s%d.md", i)] = "d\n\nb"
	}
	tests := []struct {
		name    string
		store   *fileStore
		input   string
		refused string
	}{
		{name: "not kebab-case", store: &fileStore{}, input: `{"name":"Python Module","description":"d","body":"b"}`, refused: "kebab-case"},
		{name: "past the skill limit", store: full, input: `{"name":"one-more","description":"d","body":"b"}`, refused: "16 skills"},
		{name: "replacing at the limit", store: full, input: `{"name":"s3","description":"d2","body":"b2"}`},
		{name: "well-formed", store: &fileStore{}, input: `{"name":"one-more","description":"d","body":"b"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := agent.NewScriptedModel(
				agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: agent.SkillWriteTool, Input: json.RawMessage(tt.input)}}},
				finalStep("ok"),
			)
			events := &fakeEvents{}
			s := agent.NewSession(model, newFakeClient(), agent.Options{Conversation: tt.store, Events: events})
			_, err := s.Run(context.Background(), "task")
			require.NoError(t, err)
			call := events.events[1]
			require.Equal(t, agent.EventToolCall, call.Kind)
			if tt.refused != "" {
				assert.True(t, call.ToolFailed)
				assert.Contains(t, call.ToolResult, tt.refused)
				assert.Empty(t, tt.store.writes)
				return
			}
			assert.False(t, call.ToolFailed, call.ToolResult)
			assert.Len(t, tt.store.writes, 1)
		})
	}
}
