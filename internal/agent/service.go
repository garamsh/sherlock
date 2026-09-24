package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/garamsh/gagent/internal/memory"
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
	// WorkingContext persists the current work; nil is an in-memory test session.
	WorkingContext memory.WorkingContextStorer
	Recaller       Recaller
	Writeback      WritebackSink
	Events         EventSink
	Compactor      Compactor
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
	work  memory.WorkingContext
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
			// A failed batch may leave unanswered tool calls; resume from durable work.
			if s.opts.WorkingContext != nil {
				s.history = nil
			}
			s.emit(ctx, Event{Kind: EventTurnFailed, Turn: s.turn, Error: err.Error()})
		}
	}()

	initialSource, err := s.beginWork(ctx, task)
	if err != nil {
		return "", err
	}

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
		messages, err := s.reasoningContext()
		if err != nil {
			return "", err
		}
		resp, err := s.model.Chat(ctx, messages, s.modelTools())
		if err != nil {
			return "", fmt.Errorf("model chat: %w", err)
		}
		write, calls := splitWriteback(resp.ToolCalls)
		if s.opts.WorkingContext != nil {
			if len(resp.ToolCalls) == 1 && resp.ToolCalls[0].Name == WorkingContextTool {
				if err := s.checkpoint(ctx, resp.ToolCalls[0]); err != nil {
					return "", err
				}
				s.history = append(s.history,
					Message{Role: RoleAssistant, ToolCalls: resp.ToolCalls},
					Message{Role: RoleTool, Name: WorkingContextTool, ToolCallID: resp.ToolCalls[0].ID, Content: "Checkpoint saved. Continue with operator tools or finish with memory_write."},
				)
				continue
			}
			for _, call := range resp.ToolCalls {
				if call.Name == WorkingContextTool {
					return "", fmt.Errorf("working_context must be called alone")
				}
			}
		}
		if write != nil {
			return s.finishOrRepair(ctx, resp, *write, initialSource)
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
			if err := s.execute(ctx, call); err != nil {
				return "", err
			}
		}
	}
	return "", fmt.Errorf("%w (%d)", ErrLoopExhausted, s.opts.MaxIterations)
}

// modelTools advertises reserved tools and the operator tools permitted by
// persisted terminal authority. Providers share this surface; reserved
// tools never route through the operator's client.
func (s *session) modelTools() []tool.Descriptor {
	installed := s.tools.List()
	if s.opts.WorkingContext != nil && s.work.Account.Status.Terminal() {
		installed = nil
	}
	out := make([]tool.Descriptor, 0, len(installed)+1)
	for _, d := range installed {
		if d.Name == MemoryWriteTool || (s.opts.WorkingContext != nil && d.Name == WorkingContextTool) {
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
	if s.opts.WorkingContext != nil {
		out = append(out, workingContextDescriptor())
	}
	return append(out, MemoryWriteDescriptor())
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

// finishOrRepair allows one terminal-only correction without reopening operator execution.
func (s *session) finishOrRepair(ctx context.Context, response Response, call ToolCall, initialSource bool) (string, error) {
	reply, err := s.finish(ctx, call, initialSource)
	if !errors.Is(err, ErrMalformedWriteback) && !errors.Is(err, errRejectedCuration) {
		return reply, err
	}
	kind := EventWritebackError
	if errors.Is(err, errRejectedCuration) {
		kind = EventWorkingContextError
	}
	s.emit(ctx, Event{Kind: kind, Turn: s.turn, Error: err.Error()})
	s.history = append(s.history, Message{Role: RoleAssistant, ToolCalls: response.ToolCalls})
	for _, attempted := range response.ToolCalls {
		result := "Not executed: terminal writeback repair permits no operator calls."
		if attempted.ID == call.ID {
			result = "Writeback rejected; no facts were written: " + err.Error() + ". One correction is allowed: call memory_write alone with reply and an explicit memory_ops array. Correct working_context against the supplied current-work record, or omit it to preserve all source and attempts. Resolve attempts only with observed evidence. Do not repeat operator effects."
		}
		s.history = append(s.history, Message{Role: RoleTool, Name: attempted.Name, ToolCallID: attempted.ID, Content: result})
	}
	messages, contextErr := s.reasoningContext()
	if contextErr != nil {
		return "", contextErr
	}
	repaired, repairErr := s.model.Chat(ctx, messages, []tool.Descriptor{MemoryWriteDescriptor()})
	if repairErr != nil {
		return "", fmt.Errorf("%w; terminal repair failed: %v", err, repairErr)
	}
	if len(repaired.ToolCalls) != 1 || repaired.ToolCalls[0].Name != MemoryWriteTool {
		return "", fmt.Errorf("%w; terminal repair requires memory_write alone; no operator calls executed", err)
	}
	return s.finish(ctx, repaired.ToolCalls[0], initialSource)
}

// finish applies the writeback the model ended the turn with and returns
// its reply. Malformed reply or fact data is rejected before persistence;
// finishOrRepair owns the single correction opportunity.
// A turn's history keeps the reply rather than the call that carried
// it. The writeback call is the turn's last act and nothing answers it,
// so leaving it in history leaves a tool call with no result — which a
// provider rejects on the next turn, and did: MiniMax returns HTTP 400
// "tool call result does not follow tool call" on the turn after the
// first, which made a rolling session unusable past turn one (#630).
func (s *session) finish(ctx context.Context, call ToolCall, initialSource bool) (string, error) {
	reply, ops, parseWarn, err := ParseWriteback(string(call.Input))
	if err != nil {
		return "", err
	}
	if parseWarn != nil {
		return "", parseWarn
	}
	if ops == nil {
		return "", &WritebackError{Err: errors.New("reply and memory_ops are required")}
	}
	var work memory.WorkingContext
	if s.opts.WorkingContext != nil {
		var doc struct {
			WorkingContext json.RawMessage `json:"working_context"`
		}
		if err := json.Unmarshal(call.Input, &doc); err != nil {
			return "", fmt.Errorf("decode terminal working context: %v", err)
		}
		var warning error
		work, warning, err = s.prepareCheckpoint(doc.WorkingContext)
		if err != nil {
			return "", fmt.Errorf("%w: %v", errRejectedCuration, err)
		}
		if warning != nil {
			s.emit(ctx, Event{Kind: EventWorkingContextError, Turn: s.turn, Error: warning.Error()})
		}
	}
	if err := s.applyWriteback(ctx, ops); err != nil {
		return "", err
	}
	if s.opts.WorkingContext != nil {
		// Only a fresh, otherwise idle fact write can retire its initial source.
		if initialSource && s.opts.Writeback != nil && len(ops) > 0 && work.Account.Status == memory.WorkIdle && len(work.Attempts) == 0 {
			work.Source = nil
			work.Account.Progress = ""
		}
		if err := s.saveWork(ctx, work); err != nil {
			return "", err
		}
	}
	s.history = append(s.history, Message{Role: RoleAssistant, Content: reply})
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
// model can recover. Two per-turn guards substitute a synthetic result
// instead of executing, so a stall cannot consume the whole iteration
// budget: a call past the per-tool call budget, and a call that already
// failed twice this turn with the identical error.
func (s *session) execute(ctx context.Context, call ToolCall) error {
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
		if s.opts.WorkingContext != nil {
			if err := s.recordAttempt(ctx, call); err != nil {
				return err
			}
		}
		result, event.ToolFailed = s.invoke(ctx, call)
		if s.opts.WorkingContext != nil {
			if err := s.recordResult(ctx, result, event.ToolFailed); err != nil {
				return err
			}
		}
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
	return nil
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

func (s *session) recall(ctx context.Context, task string) (Recalled, error) {
	if s.opts.Recaller == nil {
		return Recalled{}, nil
	}
	query := task
	recalled, err := s.opts.Recaller.Recall(ctx, query)
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
