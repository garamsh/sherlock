package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/garamsh/gagent/internal/tool"
)

// DefaultMaxIterations caps the act loop when Options.MaxIterations is zero.
const DefaultMaxIterations = 16

// maxToolCallsPerTurn is the per-tool call budget: calls to the same tool
// past this count within one turn are not executed. Six preserves the
// observed normal range (successful turns use up to six searches) while
// cutting pathological loops (issue #160) well short of the iteration cap.
const maxToolCallsPerTurn = 6

// Model is the agent's narrow view of a chat model (ADR 0004): it receives
// the conversation plus the registered tool schemas and returns either a
// final response or tool-call requests. Every response content ends with
// the structured memory block parsed by ParseWriteback.
type Model interface {
	Chat(ctx context.Context, messages []Message, tools []tool.Descriptor) (Response, error)
}

// ToolClient is the platform's tool client surface the session consumes
// (ADR 0018 §Tool contract). Production uses *tool.Client; tests may
// substitute any implementation that exposes the same listing and call
// surface.
type ToolClient interface {
	// List returns the descriptors (sorted by name) the model sees.
	List() []tool.Descriptor
	// Call dispatches one tool call. Validation, permission policy,
	// and output quarantine are the client's responsibility.
	Call(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error)
}

// Response is one model reply: visible content plus optional tool calls.
// Content includes the trailing memory block; the session strips it.
type Response struct {
	Content   string
	ToolCalls []ToolCall
}

// RecalledMemory is one long-term memory entry injected into context during
// the recall step, shown to the model with its ID and age.
type RecalledMemory struct {
	ID      string
	Content string
	Age     string
}

// Recaller is the recall hook point of the memory lifecycle (ADR 0006).
// The gateway wires the memory runtime's adapter; nil means no recall.
type Recaller interface {
	Recall(ctx context.Context, task string) ([]RecalledMemory, error)
}

// WritebackSink receives the memory operations parsed from every model
// response. Apply returns the existing memories most similar to the
// turn's add/update ops — the write-time coherence step (ADR 0006 step
// 5) — or nil when there is nothing to show; nil sink means ops are
// discarded.
type WritebackSink interface {
	Apply(ctx context.Context, ops []WritebackOp) ([]RecalledMemory, error)
}

// EventKind identifies a loop lifecycle event.
type EventKind string

const (
	// EventTurnStarted fires once per Run, before frame and recall.
	EventTurnStarted EventKind = "turn_started"
	// EventWriteback fires after each model response's memory block is
	// parsed and handed to the sink.
	EventWriteback EventKind = "writeback"
	// EventWritebackError fires when a model response carried a memory
	// block the parser could not turn into operations (unterminated
	// fence or invalid JSON). The turn itself continues; the warning
	// records what the model missed so subscribers can detect it. Issue
	// #93.
	EventWritebackError EventKind = "writeback_error"
	// EventToolCall fires after each tool execution.
	EventToolCall EventKind = "tool_call"
	// EventTurnFinished fires once per Run with the final response.
	EventTurnFinished EventKind = "turn_finished"
	// EventTurnFailed fires once per Run that returns an error, so the
	// monitoring stream always ends a turn with a terminal event.
	EventTurnFailed EventKind = "turn_failed"
)

// Event is one structured loop event for the gateway monitoring stream.
// Which fields are set depends on Kind: Task on turn_started, Ops on
// writeback, the Tool* fields on tool_call, Response on turn_finished,
// Error on turn_failed and writeback_error.
type Event struct {
	Kind       EventKind
	Turn       int
	Task       string
	Ops        []WritebackOp
	ToolName   string
	ToolInput  json.RawMessage
	ToolResult string
	ToolFailed bool
	Response   string
	Error      string
	// MessageIDs names the inbound messages the turn drained; the
	// gateway stamps it on terminal events (ADR 0016). The session
	// never sets it.
	MessageIDs []string
}

// EventSink receives loop events in emission order. Nil means events are
// discarded.
type EventSink interface {
	Emit(ctx context.Context, e Event)
}

// Compactor is the context-overflow hook point (ADR 0006): when history
// grows past Options.MaxHistoryMessages, the session asks the compactor to
// shrink it. The gateway wires the implementation; nil keeps history
// unbounded.
type Compactor interface {
	Compact(ctx context.Context, history []Message) ([]Message, error)
}

// Session is one rolling conversation per environment (ADR 0006): it holds
// the history and runs one turn per call to Run.
type Session interface {
	// Run executes one turn of the memory lifecycle — frame, recall, act,
	// writeback — and returns the model's final response with the memory
	// block stripped.
	Run(ctx context.Context, task string) (string, error)
	// History returns a copy of the conversation so far.
	History() []Message
}

// Options wires the session's hook points. Nil interfaces are no-ops.
type Options struct {
	Recaller  Recaller
	Writeback WritebackSink
	Events    EventSink
	Compactor Compactor
	// Ego is the identity/behavior half of the session instructions;
	// empty selects the embedded DefaultEgo. The platform Contract is
	// always appended (see ComposeInstructions).
	Ego           string
	MaxIterations int
	// MaxHistoryMessages triggers the Compactor when history exceeds it;
	// zero disables the hook.
	MaxHistoryMessages int
}

type session struct {
	model Model
	tools ToolClient
	opts  Options
	// instructions is the composed ego + contract, injected once as the
	// first system message of a fresh session.
	instructions string
	history      []Message
	turn         int
	// failures records this turn's failed tool calls, keyed by name and
	// input bytes; it resets on every Run.
	failures map[string]failedCall
	// toolCalls counts this turn's calls per tool name for the per-tool
	// call budget; it resets on every Run.
	toolCalls map[string]int
}

// failedCall is one tool call that failed in the current turn: the error
// text it produced, and whether the identical call has since failed with
// the identical error again.
type failedCall struct {
	err      string
	repeated bool
}

// NewSession returns a rolling session that drives the reasoning loop
// against model, dispatching tool calls through the protocol client
// tools. The session has no compile-time tool registry; the client
// owns the tool list and the boundary (ADR 0018 §Decision).
func NewSession(model Model, tools ToolClient, opts Options) Session {
	if opts.MaxIterations <= 0 {
		opts.MaxIterations = DefaultMaxIterations
	}
	return &session{
		model:        model,
		tools:        tools,
		opts:         opts,
		instructions: ComposeInstructions(opts.Ego),
	}
}

// History returns a copy of the conversation so far.
func (s *session) History() []Message {
	return append([]Message(nil), s.history...)
}

// Run executes one turn of the memory lifecycle: frame the task, recall
// memories into context, act (model ↔ tools until a final response), and
// write back memory operations from every model response. Every turn ends
// with exactly one terminal event: turn_finished on success, turn_failed
// on error.
func (s *session) Run(ctx context.Context, task string) (response string, err error) {
	s.turn++
	s.failures = make(map[string]failedCall)
	s.toolCalls = make(map[string]int)
	s.emit(ctx, Event{Kind: EventTurnStarted, Turn: s.turn, Task: task})
	defer func() {
		if err != nil {
			s.emit(ctx, Event{Kind: EventTurnFailed, Turn: s.turn, Error: err.Error()})
		}
	}()

	if len(s.history) == 0 {
		s.history = append(s.history, Message{Role: RoleSystem, Content: s.instructions})
	}
	recalled, err := s.recall(ctx, task)
	if err != nil {
		return "", err
	}
	s.history = append(s.history,
		Message{Role: RoleSystem, Content: frameContext(task, recalled)},
		Message{Role: RoleUser, Content: task},
	)

	for range s.opts.MaxIterations {
		if err := s.compact(ctx); err != nil {
			return "", err
		}
		resp, err := s.model.Chat(ctx, s.history, s.tools.List())
		if err != nil {
			return "", fmt.Errorf("model chat: %w", err)
		}
		content, ops, parseWarn, err := ParseWriteback(resp.Content)
		if err != nil {
			return "", err
		}
		var notes []RecalledMemory
		if parseWarn != nil {
			// A malformed writeback is a parse miss, not a fatal turn
			// error (issue #93): the visible reply still reaches the
			// user, the memory update for this turn is dropped.
			s.emit(ctx, Event{Kind: EventWritebackError, Turn: s.turn, Error: parseWarn.Error()})
		} else {
			var err error
			if notes, err = s.applyWriteback(ctx, ops); err != nil {
				return "", err
			}
		}
		s.history = append(s.history, Message{
			Role:      RoleAssistant,
			Content:   content,
			ToolCalls: resp.ToolCalls,
		})
		if len(notes) > 0 {
			s.history = append(s.history, Message{Role: RoleSystem, Content: coherenceNote(notes)})
		}
		if len(resp.ToolCalls) == 0 {
			s.emit(ctx, Event{Kind: EventTurnFinished, Turn: s.turn, Response: content})
			return content, nil
		}
		for _, call := range resp.ToolCalls {
			s.execute(ctx, call)
		}
	}
	return "", fmt.Errorf("%w (%d)", ErrLoopExhausted, s.opts.MaxIterations)
}

// execute runs one tool call through the protocol client and feeds the
// result back into history. Tool failures become tool messages so the
// model can recover. Two per-turn guards substitute a synthetic result
// instead of executing, so a stall cannot consume the whole iteration
// budget: a call past the per-tool call budget, and a call that already
// failed twice this turn with the identical error.
func (s *session) execute(ctx context.Context, call ToolCall) {
	event := Event{Kind: EventToolCall, Turn: s.turn, ToolName: call.Name, ToolInput: call.Input}
	var result string
	key := call.Name + "\x00" + string(call.Input)
	s.toolCalls[call.Name]++
	known, ok := s.failures[key]
	switch {
	case s.toolCalls[call.Name] > maxToolCallsPerTurn:
		description := descriptorDescription(s.tools.List(), call.Name)
		result = toolBudgetNotice(call.Name, description)
		event.ToolFailed = true
	case ok && known.repeated:
		result = repeatedCallNotice(call.Name, known.err)
		event.ToolFailed = true
	default:
		result, event.ToolFailed = s.invoke(ctx, call)
		if event.ToolFailed {
			s.failures[key] = failedCall{err: result, repeated: ok && known.err == result}
		}
	}
	event.ToolResult = result
	s.history = append(s.history, Message{
		Role:       RoleTool,
		Name:       call.Name,
		ToolCallID: call.ID,
		Content:    result,
	})
	s.emit(ctx, event)
}

// invoke dispatches one tool call through the protocol client, returning
// the result or the error text together with whether the call failed.
func (s *session) invoke(ctx context.Context, call ToolCall) (string, bool) {
	out, err := s.tools.Call(ctx, call.Name, call.Input)
	if err != nil {
		return err.Error(), true
	}
	return string(out), false
}

// descriptorDescription finds the named descriptor in descriptors and
// returns its description. The empty string is returned when the tool
// is not installed; the budget notice consumes it.
func descriptorDescription(descriptors []tool.Descriptor, name string) string {
	for _, d := range descriptors {
		if d.Name == name {
			return d.Description
		}
	}
	return ""
}

// repeatedCallNotice is the synthetic result substituted for a tool call
// already known to fail identically in this turn.
func repeatedCallNotice(name, err string) string {
	return fmt.Sprintf("Not executed: you already made this exact call to %q in this turn "+
		"and it failed identically every time: %s. Repeating it will not change the outcome — "+
		"change your approach or finish the turn with what you have.", name, err)
}

// toolBudgetNotice is the synthetic result substituted for a tool call past
// this turn's per-tool call budget. It restates the tool's advertised
// description so an exhausted model is steered by the tool's own contract at
// the decision point (issue #164: an exhausted memory budget must redirect
// to the writeback block, which the description already states).
func toolBudgetNotice(name, description string) string {
	notice := fmt.Sprintf("Not executed: you have reached this turn's budget of %d calls to %q. "+
		"The tool is exhausted for this turn — proceed with the results you already have "+
		"or finish the turn.", maxToolCallsPerTurn, name)
	if description != "" {
		notice += fmt.Sprintf(" (tool description: %s)", description)
	}
	return notice
}

func (s *session) recall(ctx context.Context, task string) ([]RecalledMemory, error) {
	if s.opts.Recaller == nil {
		return nil, nil
	}
	recalled, err := s.opts.Recaller.Recall(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("recall: %w", err)
	}
	return recalled, nil
}

// applyWriteback applies the parsed ops and returns the write-time
// coherence set (ADR 0006 step 5): the existing memories most similar to
// the turn's add/update ops. The note joins the history after the
// assistant message, so the next model call — the same turn when it
// continues, otherwise the next turn — judges them.
func (s *session) applyWriteback(ctx context.Context, ops []WritebackOp) ([]RecalledMemory, error) {
	var notes []RecalledMemory
	if s.opts.Writeback != nil {
		var err error
		if notes, err = s.opts.Writeback.Apply(ctx, ops); err != nil {
			return nil, fmt.Errorf("writeback: %w", err)
		}
	}
	s.emit(ctx, Event{Kind: EventWriteback, Turn: s.turn, Ops: ops})
	return notes, nil
}

// coherenceNote renders the write-time coherence set for the model.
func coherenceNote(similar []RecalledMemory) string {
	out := "## Memory coherence\nThese existing memories are similar to what you just stored. " +
		"If any contradict or are superseded by it, correct them with update/delete in your next memory block:\n"
	for _, m := range similar {
		out += fmt.Sprintf("- [%s] (%s): %s\n", m.ID, m.Age, m.Content)
	}
	return out
}

func (s *session) compact(ctx context.Context) error {
	if s.opts.Compactor == nil || s.opts.MaxHistoryMessages <= 0 ||
		len(s.history) <= s.opts.MaxHistoryMessages {
		return nil
	}
	history, err := s.opts.Compactor.Compact(ctx, s.history)
	if err != nil {
		return fmt.Errorf("compact context: %w", err)
	}
	s.history = history
	return nil
}

func (s *session) emit(ctx context.Context, e Event) {
	if s.opts.Events != nil {
		s.opts.Events.Emit(ctx, e)
	}
}

// frameContext composes the per-turn system block: the task statement
// (frame) followed by the recalled memories, with an explicit statement
// when recall came back empty (ADR 0006 step 2).
func frameContext(task string, recalled []RecalledMemory) string {
	out := "## Current task\n" + task + "\n\n## Recalled memories\n"
	if len(recalled) == 0 {
		return out + "No relevant memories."
	}
	for _, m := range recalled {
		out += fmt.Sprintf("- [%s] (%s): %s\n", m.ID, m.Age, m.Content)
	}
	return out
}
