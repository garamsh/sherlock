package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/garamsh/gagent/internal/tool"
)

// DefaultMaxIterations caps the act loop when Options.MaxIterations is zero.
const DefaultMaxIterations = 16

// Model is the agent's narrow view of a chat model (ADR 0005): it receives
// the conversation plus the registered tool schemas and returns either a
// final response or tool-call requests. Every response content ends with
// the structured memory block parsed by ParseWriteback.
type Model interface {
	Chat(ctx context.Context, messages []Message, tools []tool.Descriptor) (Response, error)
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

// Recaller is the recall hook point of the memory lifecycle (ADR 0008). The
// memory runtime satisfies it; until then a fake or nil (no recall) is fine.
type Recaller interface {
	Recall(ctx context.Context, task string) ([]RecalledMemory, error)
}

// WritebackSink receives the memory operations parsed from every model
// response. The memory runtime satisfies it; nil means ops are discarded.
type WritebackSink interface {
	Apply(ctx context.Context, ops []WritebackOp) error
}

// EventKind identifies a loop lifecycle event.
type EventKind string

const (
	// EventTurnStarted fires once per Run, before frame and recall.
	EventTurnStarted EventKind = "turn_started"
	// EventWriteback fires after each model response's memory block is
	// parsed and handed to the sink.
	EventWriteback EventKind = "writeback"
	// EventToolCall fires after each tool execution.
	EventToolCall EventKind = "tool_call"
	// EventTurnFinished fires once per Run with the final response.
	EventTurnFinished EventKind = "turn_finished"
)

// Event is one structured loop event for the gateway monitoring stream.
// Which fields are set depends on Kind: Task on turn_started, Ops on
// writeback, the Tool* fields on tool_call, Response on turn_finished.
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
}

// EventSink receives loop events in emission order. Nil means events are
// discarded.
type EventSink interface {
	Emit(ctx context.Context, e Event)
}

// Compactor is the context-overflow hook point (ADR 0008): when history
// grows past Options.MaxHistoryMessages, the session asks the compactor to
// shrink it. The memory runtime lands the real implementation; nil keeps
// history unbounded.
type Compactor interface {
	Compact(ctx context.Context, history []Message) ([]Message, error)
}

// Session is one rolling conversation per environment (ADR 0008): it holds
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
	// Persona is the identity/behavior half of the session instructions;
	// empty selects the embedded DefaultPersona. The platform Contract is
	// always appended (see ComposeInstructions).
	Persona       string
	MaxIterations int
	// MaxHistoryMessages triggers the Compactor when history exceeds it;
	// zero disables the hook.
	MaxHistoryMessages int
}

type session struct {
	model    Model
	registry *tool.Registry
	opts     Options
	// instructions is the composed persona + contract, injected once as the
	// first system message of a fresh session.
	instructions string
	history      []Message
	turn         int
}

// NewSession returns a rolling session that drives the reasoning loop
// against model, executing tool calls through registry.
func NewSession(model Model, registry *tool.Registry, opts Options) Session {
	if opts.MaxIterations <= 0 {
		opts.MaxIterations = DefaultMaxIterations
	}
	return &session{
		model:        model,
		registry:     registry,
		opts:         opts,
		instructions: ComposeInstructions(opts.Persona),
	}
}

// History returns a copy of the conversation so far.
func (s *session) History() []Message {
	return append([]Message(nil), s.history...)
}

// Run executes one turn of the memory lifecycle: frame the task, recall
// memories into context, act (model ↔ tools until a final response), and
// write back memory operations from every model response.
func (s *session) Run(ctx context.Context, task string) (string, error) {
	s.turn++
	s.emit(ctx, Event{Kind: EventTurnStarted, Turn: s.turn, Task: task})

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
		resp, err := s.model.Chat(ctx, s.history, s.registry.List())
		if err != nil {
			return "", fmt.Errorf("model chat: %w", err)
		}
		content, ops, err := ParseWriteback(resp.Content)
		if err != nil {
			return "", err
		}
		if err := s.applyWriteback(ctx, ops); err != nil {
			return "", err
		}
		s.history = append(s.history, Message{
			Role:      RoleAssistant,
			Content:   content,
			ToolCalls: resp.ToolCalls,
		})
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

// execute runs one tool call through the registry and feeds the result back
// into history. Tool failures become tool messages so the model can recover.
func (s *session) execute(ctx context.Context, call ToolCall) {
	event := Event{Kind: EventToolCall, Turn: s.turn, ToolName: call.Name, ToolInput: call.Input}
	var result string
	t, err := s.registry.Lookup(call.Name)
	if err != nil {
		result = err.Error()
		event.ToolFailed = true
	} else {
		out, err := tool.Invoke(ctx, t, call.Input)
		if err != nil {
			result = err.Error()
			event.ToolFailed = true
		} else {
			result = string(out)
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

func (s *session) applyWriteback(ctx context.Context, ops []WritebackOp) error {
	if s.opts.Writeback != nil {
		if err := s.opts.Writeback.Apply(ctx, ops); err != nil {
			return fmt.Errorf("writeback: %w", err)
		}
	}
	s.emit(ctx, Event{Kind: EventWriteback, Turn: s.turn, Ops: ops})
	return nil
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
// when recall came back empty (ADR 0008 step 2).
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
