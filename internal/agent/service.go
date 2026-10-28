package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/garamsh/sherlock/internal/tool"
)

// DefaultMaxIterations caps the act loop when Options.MaxIterations is zero.
const DefaultMaxIterations = 16

// maxToolCallsPerTurn is the per-tool call budget: calls to the same tool
// past this count within one turn are not executed. Six preserves the
// observed normal range (successful turns use up to six searches) while
// cutting pathological loops (issue #160) well short of the iteration cap.
const maxToolCallsPerTurn = 6

// Session is one rolling conversation per agent (ADR 0006, ADR 0016):
// it holds the history and runs one turn per call to Run.
type Session interface {
	// Run executes one turn — act, then writeback — and returns the
	// reply field of the model's final response.
	Run(ctx context.Context, task string) (string, error)
	// History returns a copy of the conversation so far.
	History() []Message
}

// Options wires the session's hook points. Nil interfaces are no-ops.
type Options struct {
	Events         EventSink
	Compactor      Compactor
	Conversation   ConversationMemory
	Summarizer     ConversationSummarizer
	InitialHistory []Message
	// Ego is the identity/behavior half of the session instructions;
	// empty selects the embedded DefaultEgo. The platform Contract is
	// always appended (see ComposeInstructions).
	Ego string
	// OperatorInstructions is the operator's deployment text, placed
	// between the ego and the Contract; empty omits it (ADR 0024).
	OperatorInstructions string
	MaxIterations        int
	// MaxHistoryMessages triggers the Compactor when history exceeds it;
	// zero disables the hook.
	MaxHistoryMessages int
}

type session struct {
	model Model
	tools ToolClient
	opts  Options
	// instructions is the composed ego, operator instructions and
	// contract; lead puts it, with the memory files and the skill
	// index, at the head of history every turn.
	instructions string
	history      []Message
	sources      []SourceMessage
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
		instructions: ComposeInstructions(opts.Ego, opts.OperatorInstructions),
		history:      append([]Message(nil), opts.InitialHistory...),
	}
}

// History returns a copy of the conversation so far.
func (s *session) History() []Message {
	return append([]Message(nil), s.history...)
}

// Run executes one turn: act on the task with the memory files in view,
// and persist visible conversation after the terminal response. Older
// conversation enters only through a search the model makes. Every turn ends
// with exactly one terminal event: turn_finished on success, turn_failed
// on error.
func (s *session) Run(ctx context.Context, task string) (response string, err error) {
	s.turn++
	s.sources = inboundSources(ctx, task)
	ctx = tool.WithInReplyTo(ctx, drainedIDs(s.sources))
	s.failures = make(map[string]failedCall)
	s.toolCalls = make(map[string]int)
	s.emit(ctx, Event{Kind: EventTurnStarted, Turn: s.turn, Task: task})
	defer func() {
		if err != nil {
			s.emit(ctx, Event{Kind: EventTurnFailed, Turn: s.turn, Error: err.Error()})
		}
	}()

	if err := s.lead(ctx); err != nil {
		return "", err
	}
	s.history = append(s.history, Message{Role: RoleUser, Content: task})

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
			reply, err := ParseWriteback(string(write.Input))
			if err != nil {
				return "", err
			}
			return s.finish(ctx, reply)
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
// reserved calls. The reserved descriptors are added here rather than by
// a provider so every provider advertises them the same way, and they
// are not in the tool client, so no operator tool can be reached by
// their names.
func (s *session) modelTools() []tool.Descriptor {
	installed := s.tools.List()
	out := make([]tool.Descriptor, 0, len(installed)+6)
	for _, d := range installed {
		if reserved[d.Name] {
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
		out = append(out, MemoryInspectDescriptor(), MemorySearchDescriptor(),
			MemoryFileWriteDescriptor(), SkillViewDescriptor(), SkillWriteDescriptor())
	}
	return out
}

// reserved names the calls the session answers itself.
var reserved = map[string]bool{
	MemoryWriteTool: true, MemoryInspectTool: true, MemorySearchTool: true,
	MemoryFileWriteTool: true, SkillViewTool: true, SkillWriteTool: true,
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

// finish records the turn the model ended with its reply.
// A turn's history keeps the reply rather than the call that carried
// it. The writeback call is the turn's last act and nothing answers it,
// so leaving it in history leaves a tool call with no result — which a
// provider rejects on the next turn, and did: MiniMax returns HTTP 400
// "tool call result does not follow tool call" on the turn after the
// first, which made a rolling session unusable past turn one (#630).
func (s *session) finish(ctx context.Context, reply string) (string, error) {
	s.history = append(s.history, Message{Role: RoleAssistant, Content: reply})
	if s.opts.Conversation != nil {
		if err := s.opts.Conversation.RecordTurn(ctx, append(append([]SourceMessage(nil), s.sources...), SourceMessage{Role: RoleAssistant, Content: reply}), s.opts.Summarizer); err != nil {
			return "", fmt.Errorf("record conversation: %w", err)
		}
	}
	s.emit(ctx, Event{Kind: EventWriteback, Turn: s.turn})
	s.emit(ctx, Event{Kind: EventTurnFinished, Turn: s.turn, Response: reply})
	return reply, nil
}

// writebackRequiredNotice tells the model what it has to do to end the
// turn. It names the tool rather than restating the contract, which the
// descriptor already carries.
func writebackRequiredNotice() string {
	return "Your turn is not finished. Call " + MemoryWriteTool + " with your reply."
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
		description := descriptorDescription(s.modelTools(), call.Name)
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

// memoryCalls are the reserved calls the session answers from memory.
var memoryCalls = map[string]func(context.Context, ConversationMemory, json.RawMessage) (string, error){
	MemoryInspectTool:   inspectMemory,
	MemorySearchTool:    searchMemory,
	MemoryFileWriteTool: writeMemoryFile,
	SkillViewTool:       viewSkill,
	SkillWriteTool:      writeSkill,
}

// invoke dispatches one tool call through the protocol client, returning
// the result or the error text together with whether the call failed.
func (s *session) invoke(ctx context.Context, call ToolCall) (string, bool) {
	if answer, ok := memoryCalls[call.Name]; ok && s.opts.Conversation != nil {
		out, err := answer(ctx, s.opts.Conversation, call.Input)
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

// lead puts the instructions, followed by the memory files and the skill
// index, at the head of history. It runs every turn, because the files
// can change between turns and a restored or compacted history must not
// keep stale ones.
func (s *session) lead(ctx context.Context) error {
	content := s.instructions
	if s.opts.Conversation != nil {
		files, err := s.opts.Conversation.AgentFiles(ctx)
		if err != nil {
			return fmt.Errorf("read memory files: %w", err)
		}
		content += memorySection(files)
	}
	if len(s.history) > 0 && s.history[0].Role == RoleSystem {
		s.history[0].Content = content
		return nil
	}
	s.history = append([]Message{{Role: RoleSystem, Content: content}}, s.history...)
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
