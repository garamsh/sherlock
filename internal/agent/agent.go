package agent

import (
	"encoding/json"
	"errors"
)

// Role identifies who produced a conversation message.
type Role string

const (
	// RoleSystem marks runtime-injected context (frame and recall blocks).
	RoleSystem Role = "system"
	// RoleUser marks an inbound task from the person talking to the agent.
	RoleUser Role = "user"
	// RoleAssistant marks a model response.
	RoleAssistant Role = "assistant"
	// RoleTool marks a tool execution result fed back to the model.
	RoleTool Role = "tool"
)

// ToolCall is a model's request to execute one tool.
type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// Message is one entry of conversation history. ToolCalls is set only on
// assistant messages that request tool execution; ToolCallID and Name are
// set only on tool-result messages.
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// Response is one model reply: visible content plus optional tool calls.
// The reply the person reads travels in the reserved writeback call's
// arguments, not in Content.
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

// RecallOutcome says how the read step ended, so an operator can tell a
// question memory was never asked from a question it had no answer to.
// ADR 0019 requires the two apart: the design's accuracy cannot be
// measured here, so the running system is the only place that evidence
// comes from, and a stream that collapses "memory was not consulted"
// into "memory was empty" teaches nothing.
type RecallOutcome string

const (
	// RecallUnplaced means the step reached no address: it named no
	// subject, so nothing was read.
	RecallUnplaced RecallOutcome = "unplaced"
	// RecallAddressed means the address the step placed held something.
	RecallAddressed RecallOutcome = "addressed"
	// RecallWidened means the address held nothing and the subject
	// answered instead. It is reported because ADR 0019 defers an
	// ordering until a subject read is observed to exceed the budget,
	// and this is what would show one.
	RecallWidened RecallOutcome = "widened"
	// RecallEmpty means an address was placed and neither it nor the
	// subject held anything.
	RecallEmpty RecallOutcome = "empty"
)

// Recalled is what the recall step produced: the memories injected and
// how the read reached them.
type Recalled struct {
	Memories []RecalledMemory
	Outcome  RecallOutcome
}

// MemoryAddress is where a fact is held (ADR 0018): the subject it is
// about, the question it answers, and whatever restriction the sentence
// puts on that question. A statement that narrows its question narrows
// its address with it, so the two values coexist instead of one
// destroying the other.
type MemoryAddress struct {
	Subject     string `json:"subject"`
	Question    string `json:"question"`
	Restriction string `json:"restriction,omitempty"`
}

// MemoryOperation is what a value does to the values already at its
// address: end them, or stand beside them (ADR 0018).
type MemoryOperation string

const (
	// OpReplace supersedes the values already at the address.
	OpReplace MemoryOperation = "replace"
	// OpJoin leaves them where they are and adds one.
	OpJoin MemoryOperation = "join"
)

// WritebackOp is one memory write parsed from a model response: the
// address the fact lands on, the value held there, and the operation the
// write states (ADR 0018). A write whose operation the response leaves
// unstated or unreadable parses as OpJoin, because a wrong join is
// repaired by the next correct replacement at that address and a wrong
// replacement is repaired by nothing.
type WritebackOp struct {
	Address   MemoryAddress   `json:"address"`
	Value     string          `json:"value"`
	Operation MemoryOperation `json:"operation"`
}

// EventKind identifies a loop lifecycle event.
type EventKind string

const (
	// EventTurnStarted fires once per Run, before frame and recall.
	EventTurnStarted EventKind = "turn_started"
	// EventRecall fires once per Run after the recall step, carrying the
	// injected memory IDs and their count, so subscribers see what
	// memory entered the context each turn (issue #325). Sessions
	// without a Recaller emit nothing.
	EventRecall EventKind = "recall"
	// EventWriteback fires once per turn, after the writeback call's
	// operations reach the sink.
	EventWriteback EventKind = "writeback"
	// EventWritebackError fires when a turn's writeback did not
	// arrive: the model called no writeback tool (issue #615), or
	// called it with arguments that are not the document (issue #93).
	// The first is not terminal — the session says what is missing and
	// asks again — and the second permits one terminal-only correction.
	EventWritebackError EventKind = "writeback_error"
	// EventWorkingContextError reports optional curation rejected while durable work is retained.
	EventWorkingContextError EventKind = "working_context_error"
	// EventToolCall fires after each tool execution.
	EventToolCall EventKind = "tool_call"
	// EventTurnFinished fires once per Run with the final response.
	EventTurnFinished EventKind = "turn_finished"
	// EventTurnFailed fires once per Run that returns an error, so the
	// monitoring stream always ends a turn with a terminal event.
	EventTurnFailed EventKind = "turn_failed"
)

// Event is one structured loop event for the gateway monitoring stream.
// Which fields are set depends on Kind: Task on turn_started,
// RecalledIDs and RecalledCount on recall, Ops on writeback, the Tool*
// fields on tool_call, Response on turn_finished, Error on turn_failed
// and writeback_error or working_context_error.
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
	// RecalledIDs names the memories the recall step injected this turn;
	// RecalledCount is their number.
	RecalledIDs   []string
	RecalledCount int
	// RecallOutcome says how the read step reached what it injected, so
	// an unplaced question and an empty address are distinguishable on
	// the stream (ADR 0019). Empty on every event but recall.
	RecallOutcome RecallOutcome
	// MessageIDs names the inbound messages the turn drained; the
	// gateway stamps it on terminal events (ADR 0015). The session
	// never sets it.
	MessageIDs []string
}

var (
	// ErrScriptExhausted is returned by the scripted model when a test
	// scenario asks for more responses than were scripted.
	ErrScriptExhausted = errors.New("scripted model: script exhausted")
	// ErrWritebackNotCalled marks a model response that ended nothing:
	// no reserved writeback call and no tool call to run. It is the
	// shape a provider produces when it ignores the tools it was sent,
	// which is how MiniMax ignores a response format (issue #615). The
	// session does not end a turn on it — it says so and asks again —
	// so this reaches a subscriber as the reason a turn is still
	// running rather than as the reason it failed.
	ErrWritebackNotCalled = errors.New("model ended a turn without calling " + MemoryWriteTool)
	// ErrLoopExhausted is returned when the act loop exceeds the iteration
	// cap without the model producing a final response.
	ErrLoopExhausted = errors.New("act loop exceeded max iterations")
)

// ErrMalformedWriteback marks writeback arguments the contract refuses:
// a JSON parse error or a schema violation. One terminal-only repair is allowed. The
// leniency issue #93 asked for is gone: the arguments are the only place
// the reply and the facts arrive, so a malformed one leaves nothing to
// hand back and nothing to store (issue #615).
var ErrMalformedWriteback = errors.New("malformed memory writeback")

// errRejectedCuration marks an unsafe final update rejected before any write.
var errRejectedCuration = errors.New("terminal working context rejected")
