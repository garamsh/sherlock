package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/garamsh/gagent/internal/tool"
)

// DefaultMaxIterations caps the act loop when Options.MaxIterations is zero.
const DefaultMaxIterations = 16

// Model is the agent's narrow view of a chat model (ADR 0004): it receives
// the conversation plus the registered tool schemas and returns either a
// final response or tool-call requests. A turn ends when the model calls
// the reserved MemoryWriteTool, whose arguments are the document
// ResponseSchema describes, parsed by ParseWriteback.
type Model interface {
	Chat(ctx context.Context, messages []Message, tools []tool.Descriptor) (Response, error)
}

// ToolClient is the platform's tool client surface the session consumes
// (ADR 0017 §Tool contract). Production uses *tool.Registry; tests may
// substitute any implementation that exposes the same listing and call
// surface.
type ToolClient interface {
	// List returns the descriptors (sorted by name) the model sees.
	List() []tool.Descriptor
	// Call dispatches one tool call. Validation and permission policy
	// are the client's responsibility, and so is holding a
	// non-conforming output back: a call whose output fails its schema
	// returns an error and the output does not reach the model.
	Call(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error)
}

// Recaller is the recall hook point of the memory lifecycle (ADR 0006).
// internal/agent/memory implements it over the memory service and the
// runtime wires it; nil means no recall.
type Recaller interface {
	Recall(ctx context.Context, task string) (Recalled, error)
}

// WritebackSink receives the memory operations the turn ended with. A
// nil sink means ops are discarded.
//
// It returns no coherence set. ADR 0006 step 5 showed the model the
// memories most similar to what it had just written, which #582 removed
// from the shipped sink when the address made similarity decide nothing;
// a turn now ends on the writeback, so there is no later call within it
// that could have read one.
type WritebackSink interface {
	Apply(ctx context.Context, ops []WritebackOp) error
}

// EventSink receives loop events in emission order. Nil means events are
// discarded.
type EventSink interface {
	Emit(ctx context.Context, e Event)
}

// Compactor is the context-overflow hook point (ADR 0006): when history
// grows past Options.MaxHistoryMessages, the session asks the compactor to
// shrink it. internal/agent/memory implements it; nil keeps history
// unbounded.
type Compactor interface {
	Compact(ctx context.Context, history []Message) ([]Message, error)
}

// Session is one rolling conversation per agent (ADR 0006, ADR 0016):
// it holds the history and runs one turn per call to Run.
type Session interface {
	// Run executes one turn of the memory lifecycle — frame, recall, act,
	// writeback — and returns the reply field of the model's final
	// response.
	Run(ctx context.Context, task string) (string, error)
	// History returns a copy of the conversation so far.
	History() []Message
}

// Options wires the session's hook points. Nil interfaces are no-ops.
type Options struct {
	Recaller       Recaller
	Writeback      WritebackSink
	Events         EventSink
	Compactor      Compactor
	Conversation   ConversationMemory
	Summarizer     ConversationSummarizer
	InitialHistory []Message
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
	sources      []SourceMessage
	turn         int
	// failures records this turn's failed tool calls, keyed by name and
	// input bytes; it resets on every Run.
	failures map[string]failedCall
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
// owns the tool list and the boundary (ADR 0017 §Decision).
func NewSession(model Model, tools ToolClient, opts Options) Session {
	if opts.MaxIterations <= 0 {
		opts.MaxIterations = DefaultMaxIterations
	}
	return &session{
		model:        model,
		tools:        tools,
		opts:         opts,
		instructions: ComposeInstructions(opts.Ego),
		history:      append([]Message(nil), opts.InitialHistory...),
	}
}

// History returns a copy of the conversation so far.
func (s *session) History() []Message {
	return append([]Message(nil), s.history...)
}

// Run executes one turn: frame the task, recall linked candidates, act,
// and persist visible conversation after the terminal response. Every turn ends
// with exactly one terminal event: turn_finished on success, turn_failed
// on error.
func (s *session) Run(ctx context.Context, task string) (response string, err error) {
	s.turn++
	s.sources = inboundSources(ctx, task)
	s.failures = make(map[string]failedCall)
	s.emit(ctx, Event{Kind: EventTurnStarted, Turn: s.turn, Task: task})
	defer func() {
		if err != nil {
			s.emit(ctx, Event{Kind: EventTurnFailed, Turn: s.turn, Error: err.Error()})
		}
	}()

	if len(s.history) == 0 || s.history[0].Role != RoleSystem {
		s.history = append([]Message{{Role: RoleSystem, Content: s.instructions}}, s.history...)
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
		resp, err := s.model.Chat(ctx, s.history, s.modelTools())
		if err != nil {
			return "", fmt.Errorf("model chat: %w", err)
		}
		write, calls := splitWriteback(resp.ToolCalls)
		if write != nil {
			return s.finish(ctx, *write)
		}
		s.history = append(s.history, Message{
			Role:      RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})
		if len(calls) == 0 {
			// The model tried to end the turn without writing. It is
			// not ended: a turn that returns a reply and stores
			// nothing is the 0.00 write path #336 step 1 removed, and
			// a provider that ignores the tool must fail loudly rather
			// than look like a turn that stated no facts (issue #615).
			s.emit(ctx, Event{Kind: EventWritebackError, Turn: s.turn, Error: ErrWritebackNotCalled.Error()})
			s.history = append(s.history, Message{Role: RoleSystem, Content: writebackRequiredNotice()})
			continue
		}
		for _, call := range calls {
			s.execute(ctx, call)
		}
	}
	return "", fmt.Errorf("%w (%d)", ErrLoopExhausted, s.opts.MaxIterations)
}

// modelTools is what the model is shown: the operator's tools plus the
// reserved writeback. The reserved descriptor is added here rather than
// by a provider so every provider advertises it the same way, and it is
// not in the tool client, so no operator tool can be reached by that
// name.
func (s *session) modelTools() []tool.Descriptor {
	installed := s.tools.List()
	out := make([]tool.Descriptor, 0, len(installed)+2)
	for _, d := range installed {
		if d.Name == MemoryWriteTool || d.Name == MemoryInspectTool {
			// An operator tool cannot take the reserved name: the
			// session would route its calls to the writeback and the
			// operator's tool would never run. Dropping it is visible
			// in the log; shadowing it silently is not.
			slog.Warn("agent: operator tool shadows the reserved writeback name, dropped",
				"op", "model_tools", "tool", d.Name)
			continue
		}
		out = append(out, d)
	}
	out = append(out, MemoryWriteDescriptor())
	if s.opts.Conversation != nil {
		out = append(out, MemoryInspectDescriptor())
	}
	return out
}

// splitWriteback separates the reserved writeback call from the tool
// calls the client executes. Only the first writeback call is taken; a
// model that asks twice in one response has stated one turn's facts and
// the rest are dropped with the turn.
func splitWriteback(calls []ToolCall) (*ToolCall, []ToolCall) {
	var write *ToolCall
	rest := make([]ToolCall, 0, len(calls))
	for i, call := range calls {
		if call.Name == MemoryWriteTool && write == nil {
			write = &calls[i]
			continue
		}
		if call.Name == MemoryWriteTool {
			continue
		}
		rest = append(rest, call)
	}
	return write, rest
}

// finish applies the writeback the model ended the turn with and returns
// its reply. A writeback whose arguments are not the document is fatal
// here where a malformed response was once a warn: the arguments are the
// only place the turn's reply and its facts arrive, so there is nothing
// to hand back and nothing to store (issue #615).
// A turn's history keeps the reply rather than the call that carried
// it. The writeback call is the turn's last act and nothing answers it,
// so leaving it in history leaves a tool call with no result — which a
// provider rejects on the next turn, and did: MiniMax returns HTTP 400
// "tool call result does not follow tool call" on the turn after the
// first, which made a rolling session unusable past turn one (#630).
func (s *session) finish(ctx context.Context, call ToolCall) (string, error) {
	reply, ops, parseWarn, err := ParseWriteback(string(call.Input))
	if err != nil {
		return "", err
	}
	s.history = append(s.history, Message{Role: RoleAssistant, Content: reply})
	if parseWarn != nil {
		s.emit(ctx, Event{Kind: EventWritebackError, Turn: s.turn, Error: parseWarn.Error()})
		return "", parseWarn
	}
	if err := s.applyWriteback(ctx, ops); err != nil {
		return "", err
	}
	if s.opts.Conversation != nil {
		if err := s.opts.Conversation.RecordTurn(ctx, append(append([]SourceMessage(nil), s.sources...), SourceMessage{Role: RoleAssistant, Content: reply}), s.opts.Summarizer); err != nil {
			return "", fmt.Errorf("record conversation: %w", err)
		}
	}
	s.emit(ctx, Event{Kind: EventTurnFinished, Turn: s.turn, Response: reply})
	return reply, nil
}

// writebackRequiredNotice tells the model what it has to do to end the
// turn. It names the tool rather than restating the contract, which the
// descriptor already carries.
func writebackRequiredNotice() string {
	return "Your turn is not finished. Call " + MemoryWriteTool +
		" with your reply and the facts this turn stated; the empty list is a valid answer."
}

// execute runs one tool call through the protocol client and feeds the
// result back into history. Tool failures become tool messages so the
// model can recover. An identical failure repeated twice this turn receives
// a synthetic result; successful calls have no per-tool allowance. The
// whole-turn iteration ceiling remains the backstop.
func (s *session) execute(ctx context.Context, call ToolCall) {
	event := Event{Kind: EventToolCall, Turn: s.turn, ToolName: call.Name, ToolInput: call.Input}
	var result string
	key := call.Name + "\x00" + string(call.Input)
	known, ok := s.failures[key]
	switch {
	case ok && known.repeated:
		result = repeatedCallNotice(call.Name, known.err)
		event.ToolFailed = true
	default:
		result, event.ToolFailed = s.invoke(ctx, call)
		if event.ToolFailed {
			s.failures[key] = failedCall{err: result, repeated: ok && known.err == result}
		}
	}
	if !event.ToolFailed && call.Name == "message_send" {
		var sent struct {
			Content string `json:"content"`
		}
		if json.Unmarshal(call.Input, &sent) == nil && sent.Content != "" {
			s.sources = append(s.sources, SourceMessage{Role: RoleAssistant, Content: sent.Content})
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
	if call.Name == MemoryInspectTool && s.opts.Conversation != nil {
		out, err := inspectMemory(ctx, s.opts.Conversation, call.Input)
		if err != nil {
			return err.Error(), true
		}
		return out, false
	}
	out, err := s.tools.Call(ctx, call.Name, call.Input)
	if err != nil {
		return err.Error(), true
	}
	return string(out), false
}

// repeatedCallNotice is the synthetic result substituted for a tool call
// already known to fail identically in this turn.
func repeatedCallNotice(name, err string) string {
	return fmt.Sprintf("Not executed: you already made this exact call to %q in this turn "+
		"and it failed identically every time: %s. Repeating it will not change the outcome — "+
		"change your approach or finish the turn with what you have.", name, err)
}

func (s *session) recall(ctx context.Context, task string) (Recalled, error) {
	if s.opts.Recaller == nil {
		return Recalled{}, nil
	}
	recalled, err := s.opts.Recaller.Recall(ctx, task)
	if err != nil {
		return Recalled{}, fmt.Errorf("recall: %w", err)
	}
	var ids []string
	for _, m := range recalled.Memories {
		ids = append(ids, m.ID)
	}
	s.emit(ctx, Event{
		Kind:          EventRecall,
		Turn:          s.turn,
		RecalledIDs:   ids,
		RecalledCount: len(ids),
		RecallOutcome: recalled.Outcome,
	})
	return recalled, nil
}

// applyWriteback applies the parsed ops the turn ended with. The
// coherence set the sink returns is no longer read: a turn ends on the
// writeback, so there is no later model call in it to judge one, and
// #582 already left the shipped sink returning none.
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
// when recall came back empty (ADR 0006 step 2).
// frameContext renders the turn's task and what recall reached. The two
// emptinesses are stated apart (ADR 0019): a question memory could not
// place is not a question memory had no answer to, and a model told the
// second when the first happened is being told something false.
func frameContext(task string, recalled Recalled) string {
	out := "## Current task\n" + task + "\n\n## Recalled memories\n"
	if len(recalled.Memories) == 0 {
		switch recalled.Outcome {
		case RecallUnplaced:
			return out + "Memory was not consulted: this task did not resolve to an address."
		default:
			return out + "No relevant memories."
		}
	}
	for _, m := range recalled.Memories {
		out += fmt.Sprintf("- [%s] (%s): %s\n", m.ID, m.Age, m.Content)
	}
	return out
}
