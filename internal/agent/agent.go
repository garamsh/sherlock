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

var (
	// ErrScriptExhausted is returned by the scripted model when a test
	// scenario asks for more responses than were scripted.
	ErrScriptExhausted = errors.New("scripted model: script exhausted")
	// ErrLoopExhausted is returned when the act loop exceeds the iteration
	// cap without the model producing a final response.
	ErrLoopExhausted = errors.New("act loop exceeded max iterations")
)
