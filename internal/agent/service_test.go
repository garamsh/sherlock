package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/tool"
)

// replyOnly renders the response document (agent.ResponseSchema) for a
// turn whose reply states no memory writes.
func replyOnly(text string) string {
	out, err := json.Marshal(struct {
		Reply string              `json:"reply"`
		Ops   []agent.WritebackOp `json:"memory_ops"`
	}{Reply: text, Ops: []agent.WritebackOp{}})
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

// fakeRecaller scripts the recall hook: it returns fixed memories and
// records the task statements it was queried with.
type fakeRecaller struct {
	memories []agent.RecalledMemory
	tasks    []string
	outcome  agent.RecallOutcome
	err      error
}

func (f *fakeRecaller) Recall(_ context.Context, task string) (agent.Recalled, error) {
	f.tasks = append(f.tasks, task)
	if f.err != nil {
		return agent.Recalled{}, f.err
	}
	outcome := f.outcome
	if outcome == "" {
		outcome = agent.RecallAddressed
	}
	return agent.Recalled{Memories: f.memories, Outcome: outcome}, nil
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
	// reserved writeback carrying both facts it stated. A turn writes
	// once, when it ends, rather than once per model response.
	model := agent.NewScriptedModel(
		agent.ScriptedStep{
			Content: "Let me check.",
			ToolCalls: []agent.ToolCall{
				{Name: "echo", Input: json.RawMessage(`{"text":"hi"}`)},
			},
		},
		agent.ScriptedStep{
			ToolCalls: []agent.ToolCall{writebackCall(
				`{"reply":"The echo said hi.","memory_ops":[{"address":{"subject":"user",` +
					`"question":"which drinks does the user like","restriction":null},` +
					`"value":"tea","operation":"join"},{"address":{"subject":"user",` +
					`"question":"which team does the user sit with","restriction":"since june"},` +
					`"value":"platform","operation":"replace"}]}`)},
		},
	)
	recaller := &fakeRecaller{memories: []agent.RecalledMemory{
		{ID: "m1", Content: "user drinks tea", Age: "2d"},
	}}
	writeback := &fakeWriteback{}
	events := &fakeEvents{}

	s := agent.NewSession(model, newFakeClientWith(newEchoTool()), agent.Options{
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

	// The turn's memory writes reached the sink, in one batch: the
	// writeback is the call the turn ends on.
	require.Equal(t, [][]agent.WritebackOp{
		{
			{
				Address: agent.MemoryAddress{
					Subject:  "user",
					Question: "which drinks does the user like",
				},
				Value:     "tea",
				Operation: agent.OpJoin,
			},
			{
				Address: agent.MemoryAddress{
					Subject:     "user",
					Question:    "which team does the user sit with",
					Restriction: "since june",
				},
				Value:     "platform",
				Operation: agent.OpReplace,
			},
		},
	}, writeback.batches)

	// Events fired in order within the turn.
	require.Equal(t, []agent.EventKind{
		agent.EventTurnStarted,
		agent.EventRecall,
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
	assert.Len(t, s.History(), 7)
	assert.Equal(t, 1, events.events[0].Turn)
	last := events.events[len(events.events)-1]
	assert.Equal(t, agent.EventTurnFinished, last.Kind)
	assert.Equal(t, 2, last.Turn)
}

func TestSessionRun_NoRecallStatesAbsence(t *testing.T) {
	model := agent.NewScriptedModel(
		finalStep("ok"),
	)
	s := agent.NewSession(model, newFakeClient(), agent.Options{})

	_, err := s.Run(context.Background(), "anything")
	require.NoError(t, err)
	assert.Contains(t, s.History()[1].Content, "No relevant memories.")
}

// TestSessionRun_RecallEventEmitted pins issue #325: a turn whose recall
// returns memories emits exactly one recall event carrying the injected
// IDs and their count, so the monitoring stream shows what memory
// entered the context.
func TestSessionRun_RecallEventEmitted(t *testing.T) {
	model := agent.NewScriptedModel(
		finalStep("done"),
	)
	recaller := &fakeRecaller{memories: []agent.RecalledMemory{
		{ID: "m1", Content: "user drinks tea", Age: "2d"},
		{ID: "m2", Content: "user lives in Lisbon", Age: "5d"},
	}}
	events := &fakeEvents{}
	s := agent.NewSession(model, newFakeClient(), agent.Options{
		Recaller: recaller,
		Events:   events,
	})

	_, err := s.Run(context.Background(), "what does the user drink?")
	require.NoError(t, err)

	var recallEvents []agent.Event
	for _, e := range events.events {
		if e.Kind == agent.EventRecall {
			recallEvents = append(recallEvents, e)
		}
	}
	require.Len(t, recallEvents, 1, "exactly one recall event per turn")
	assert.Equal(t, []string{"m1", "m2"}, recallEvents[0].RecalledIDs)
	assert.Equal(t, 2, recallEvents[0].RecalledCount)
	assert.Equal(t, 1, recallEvents[0].Turn)
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

// TestSessionRun_TheTwoEmptinessesReachTheModelApart is ADR 0019's
// split held where it matters: what the model is told. A question memory
// could not place is not a question memory had no answer to, and a model
// told the second when the first happened is being told something false.
// The recall event carries the same distinction for the operator.
func TestSessionRun_TheTwoEmptinessesReachTheModelApart(t *testing.T) {
	tests := []struct {
		name      string
		outcome   agent.RecallOutcome
		wantFrame string
		notFrame  string
	}{
		{
			name:      "placed nothing",
			outcome:   agent.RecallUnplaced,
			wantFrame: "did not resolve to an address",
			notFrame:  "No relevant memories.",
		},
		{
			name:      "held nothing",
			outcome:   agent.RecallEmpty,
			wantFrame: "No relevant memories.",
			notFrame:  "did not resolve to an address",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := agent.NewScriptedModel(finalStep("ok"))
			events := &fakeEvents{}
			s := agent.NewSession(model, newFakeClient(), agent.Options{
				Recaller: &fakeRecaller{outcome: tt.outcome},
				Events:   events,
			})

			_, err := s.Run(context.Background(), "task")
			require.NoError(t, err)

			var frame string
			for _, m := range s.History() {
				if strings.Contains(m.Content, "## Recalled memories") {
					frame = m.Content
				}
			}
			require.NotEmpty(t, frame)
			assert.Contains(t, frame, tt.wantFrame)
			assert.NotContains(t, frame, tt.notFrame)

			var recall agent.Event
			for _, e := range events.events {
				if e.Kind == agent.EventRecall {
					recall = e
				}
			}
			require.Equal(t, agent.EventRecall, recall.Kind, "the turn emits a recall event")
			assert.Equal(t, tt.outcome, recall.RecallOutcome,
				"the stream carries which emptiness it was")
		})
	}
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
	writeback := &fakeWriteback{}
	s := agent.NewSession(model, newFakeClient(), agent.Options{
		Events:    events,
		Writeback: writeback,
	})

	final, err := s.Run(context.Background(), "hello")
	require.NoError(t, err, "the model complied on the second call")
	assert.Equal(t, "Hello!", final)

	// The turn wrote, because it ended the only way a turn can.
	require.Len(t, writeback.batches, 1)

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
	writeback := &fakeWriteback{}
	s := agent.NewSession(agent.NewScriptedModel(steps...), newFakeClient(), agent.Options{
		Events:        events,
		Writeback:     writeback,
		MaxIterations: 4,
	})

	final, err := s.Run(context.Background(), "task")
	require.Error(t, err, "a turn that never writes fails rather than replying")
	assert.True(t, errors.Is(err, agent.ErrLoopExhausted))
	assert.Empty(t, final)
	assert.Empty(t, writeback.batches)

	terminal := events.events[len(events.events)-1]
	assert.Equal(t, agent.EventTurnFailed, terminal.Kind)
}

// TestSessionRun_MalformedWritebackArgumentsFailTheTurn pins where the
// leniency issue #93 asked for now sits. The arguments are the only
// place the reply and the facts arrive, so arguments that are not the
// document leave nothing to hand back and nothing to store: the turn
// fails rather than returning a reply that was never written down.
func TestSessionRun_MalformedWritebackArgumentsFailTheTurn(t *testing.T) {
	model := agent.NewScriptedModel(
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{writebackCall(`{"reply":"still useful","memory_ops":[`)}},
	)
	events := &fakeEvents{}
	writeback := &fakeWriteback{}
	s := agent.NewSession(model, newFakeClient(), agent.Options{
		Events:    events,
		Writeback: writeback,
	})

	_, err := s.Run(context.Background(), "task")
	require.Error(t, err)
	assert.True(t, errors.Is(err, agent.ErrMalformedWriteback))
	assert.Empty(t, writeback.batches)

	terminal := events.events[len(events.events)-1]
	assert.Equal(t, agent.EventTurnFailed, terminal.Kind)
}

// TestSessionRun_StrictWritebackSchema is the contract-strict sibling
// to TestSessionRun_LenientWritebackParse: a writeback whose JSON parses
// but states a write with nowhere to land still fails the turn, so the
// model learns the contract (issue #93).
func TestSessionRun_StrictWritebackSchema(t *testing.T) {
	model := agent.NewScriptedModel(
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{writebackCall(
			`{"reply":"broken","memory_ops":[{"address":` +
				`{"subject":"","question":"","restriction":null},` +
				`"value":"","operation":null}]}`)}},
	)
	events := &fakeEvents{}
	s := agent.NewSession(model, newFakeClient(), agent.Options{Events: events})

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

	toolMsg := s.History()[4]
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
	require.GreaterOrEqual(t, len(messages), 3)
	assert.Equal(t, agent.RoleSystem, messages[0].Role)
	assert.Equal(t, agent.ComposeInstructions(""), messages[0].Content)
	assert.Equal(t, agent.RoleSystem, messages[1].Role, "frame context follows the instructions")
	assert.Equal(t, agent.RoleUser, messages[2].Role)
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
		if msg.Content == agent.ComposeInstructions("") {
			injections++
		}
	}
	assert.Equal(t, 1, injections, "instructions are injected once, on the first turn")
	require.Len(t, model.calls, 2)
}
