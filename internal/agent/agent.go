package agent

import (
	"encoding/json"
	"errors"
)

// Role identifies who produced a conversation message.
type Role string

const (
	// RoleSystem marks runtime-injected context (the instructions and memory files).
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

// EventKind identifies a loop lifecycle event.
type EventKind string

const (
	// EventTurnStarted fires once per Run, before the act loop.
	EventTurnStarted EventKind = "turn_started"
	// EventWriteback fires once per completed turn, after its sources
	// are saved.
	EventWriteback EventKind = "writeback"
	// EventWritebackError fires when a turn's writeback did not
	// arrive: the model called no writeback tool (issue #615), or called
	// it with arguments that are not JSON (issue #93). The first is not
	// terminal — the session says what is wrong and asks again — and the
	// second fails the turn.
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
// Which fields are set depends on Kind: Task on turn_started, the Tool*
// fields on tool_call, Response on turn_finished, Error on turn_failed
// and writeback_error.
type Event struct {
	Kind       EventKind
	Turn       int
	Task       string
	ToolName   string
	ToolInput  json.RawMessage
	ToolResult string
	ToolFailed bool
	Response   string
	Error      string
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

// ErrMalformedWriteback marks terminal-call arguments that are not JSON.
// It fails the turn: the arguments are the only place the reply arrives,
// so a malformed one leaves nothing to hand back (issue #615).
var ErrMalformedWriteback = errors.New("malformed memory writeback")
