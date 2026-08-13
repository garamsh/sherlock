// Package tool is the platform's tool client over the platform protocol
// (ADR 0018 §Decision, ADR 0009 §Decision). It discovers installed tool
// servers through the operator's tool list, validates descriptors at the
// boundary, pins descriptor hashes to refuse drift at startup, applies
// a per-tool permission policy, and dispatches tool calls.
package tool

import (
	"encoding/json"
	"fmt"
)

// JSON-RPC 2.0 framing for the platform protocol. The transport is stdio
// over os/exec — one process per tool server, one newline-delimited JSON
// message per line in each direction. The platform specifies the two
// methods it needs (tools/list, tools/call); tools are free to honour
// other JSON-RPC methods the platform ignores.

// request is one JSON-RPC 2.0 request the client sends to a tool server.
type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// response is one JSON-RPC 2.0 response the client expects from a tool
// server. Result is the success payload; Error is set on failure.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *responseError  `json:"error,omitempty"`
}

// responseError is the JSON-RPC 2.0 error object attached to a failed
// response. Code is the standard JSON-RPC error code; Message is the
// human-readable text; Data is the optional tool-specific payload.
type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC 2.0 error codes (https://www.jsonrpc.org/specification).
// Tool servers use these codes; the platform client translates them
// when surfacing typed errors.
const (
	// ErrCodeParseError (-32700) marks a malformed JSON payload.
	ErrCodeParseError = -32700
	// ErrCodeInvalidRequest (-32600) marks a JSON-RPC envelope that
	// does not match the spec.
	ErrCodeInvalidRequest = -32600
	// ErrCodeMethodNotFound (-32601) marks an unknown method.
	ErrCodeMethodNotFound = -32601
	// ErrCodeInvalidParams (-32602) marks parameters that do not
	// validate against the tool's input schema.
	ErrCodeInvalidParams = -32602
	// ErrCodeInternalError (-32603) marks an exception inside the tool.
	ErrCodeInternalError = -32603
)

// listParams is the params block for tools/list. The MCP spec allows
// pagination; the platform keeps the minimum — cursor empty, fetch all.
type listParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// toolDescriptor is one tool the server reports in response to tools/list.
// MCP-compatible field names so third-party servers that follow the
// shape integrate without a custom mapping.
type toolDescriptor struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
}

// listResult is the result block for tools/list.
type listResult struct {
	Tools      []toolDescriptor `json:"tools"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

// callParams is the params block for tools/call.
type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// contentBlock is one entry in the tools/call result content array. The
// platform reads the first text block; structured JSON lives inside the
// text block as the raw tool output payload.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// callResult is the result block for tools/call. IsError is the tool's
// own verdict; the platform still re-validates the output against the
// declared output schema (ADR 0018 §Tool contract).
type callResult struct {
	Content  []contentBlock `json:"content"`
	IsError  bool           `json:"isError,omitempty"`
	Metadata map[string]any `json:"_meta,omitempty"`
}

// callError wraps a JSON-RPC error returned by a tool server, with the
// method that produced it so the caller can route the error.
func (e *responseError) callError(method string) error {
	return fmt.Errorf("tool server %s: %s (code %d)", method, e.Message, e.Code)
}
