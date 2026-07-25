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

// OpKind identifies one memory writeback operation.
type OpKind string

const (
	// OpKeep marks a recalled memory as still valid, unchanged.
	OpKeep OpKind = "keep"
	// OpUpdate supersedes a recalled memory with new content.
	OpUpdate OpKind = "update"
	// OpDelete retires a recalled memory.
	OpDelete OpKind = "delete"
	// OpAdd persists a new fact.
	OpAdd OpKind = "add"
)

// WritebackOp is one parsed operation from the memory block that ends every
// model response (ADR 0008). ID is set for keep/update/delete; Content for
// update/add; Type for add.
type WritebackOp struct {
	Kind    OpKind `json:"kind"`
	ID      string `json:"id,omitempty"`
	Content string `json:"content,omitempty"`
	Type    string `json:"type,omitempty"`
}

var (
	// ErrScriptExhausted is returned by the scripted model when a test
	// scenario asks for more responses than were scripted.
	ErrScriptExhausted = errors.New("scripted model: script exhausted")
	// ErrLoopExhausted is returned when the act loop exceeds the iteration
	// cap without the model producing a final response.
	ErrLoopExhausted = errors.New("act loop exceeded max iterations")
)
