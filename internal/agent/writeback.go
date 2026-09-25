package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/garamsh/sherlock/internal/tool"
)

// MemoryWriteTool is the reserved tool a turn ends by calling. It is the
// platform's, never an operator's: the session adds its descriptor to
// what the model is shown, intercepts the call, and never routes it to
// the tool client (ADR 0017 leaves the operator's directory to the
// operator).
//
// It carries the writeback because a response format does not. MiniMax
// accepts `response_format: json_schema` with HTTP 200 and ignores it on
// every model tried, which returned the write path to the 0.00 that
// #336 step 1 exists to remove — silently, because a response that is
// not the document parses to no operations and the turn otherwise
// succeeds (issue #615). Tool calls are honoured where the response
// format is not, and they are the same constrained decoding each tool's
// parameters already run under.
const MemoryWriteTool = "memory_write"

// memoryWriteDescription tells the model what the tool is for and that
// the turn ends through it. It is the descriptor's text rather than the
// session instructions because a tool the model is shown carries its own
// contract, and because what a caller may decline is not settled by
// prose: the session will not end a turn without this call.
const memoryWriteDescription = "Deliver your reply. " +
	"Every turn ends with this call and no turn ends without it. The reply is what the person reads; " +
	"the runtime saves visible conversation separately."

// MemoryWriteDescriptor is the reserved tool as the model sees it. Its
// parameters are ResponseSchema, so what the write carries is the same
// document whichever transport delivers it.
func MemoryWriteDescriptor() tool.Descriptor {
	return tool.Descriptor{
		Name:        MemoryWriteTool,
		Description: memoryWriteDescription,
		InputSchema: json.RawMessage(ResponseSchema),
	}
}

// ResponseSchema is the JSON Schema the terminal call is constrained to.
// It is the reserved tool's parameters, and ParseWriteback reads what the
// model puts in them, so the schema and the parser are one contract and
// are kept together.
const ResponseSchema = `{
  "type": "object",
  "properties": {
    "reply": {
      "type": "string",
      "description": "Everything the person reads."
    }
  },
  "required": ["reply"],
  "additionalProperties": false
}`

// WritebackError reports terminal-call arguments that are not the
// document. It matches ErrMalformedWriteback via errors.Is, and the
// session fails the turn on it: the arguments are the only place the
// reply arrives (issue #615).
type WritebackError struct {
	Err error
}

// Error returns a human-readable description of the parse failure.
func (e *WritebackError) Error() string {
	return fmt.Sprintf("%v: %v", ErrMalformedWriteback, e.Err)
}

// Unwrap returns the underlying parse failure.
func (e *WritebackError) Unwrap() error { return e.Err }

// Is reports whether target is ErrMalformedWriteback.
func (e *WritebackError) Is(target error) bool { return target == ErrMalformedWriteback }

// writebackJSON is the wire shape of the terminal call.
type writebackJSON struct {
	Reply string `json:"reply"`
}

// ParseWriteback reads the visible reply out of the terminal call's
// arguments. Arguments that are not JSON fail with *WritebackError.
func ParseWriteback(content string) (string, error) {
	var doc writebackJSON
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &doc); err != nil {
		return "", &WritebackError{Err: fmt.Errorf("invalid JSON: %s", jsonErrorMessage(err))}
	}
	return strings.TrimSpace(doc.Reply), nil
}

// jsonErrorMessage returns a model-facing message for a JSON unmarshal
// failure. It strips the standard library's "Go struct field ..." suffix,
// which leaks unexported Go type names that are not part of the wire
// contract. The leading "invalid JSON: " prefix is added by the caller.
func jsonErrorMessage(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, "Go struct field"); i >= 0 {
		msg = strings.TrimSpace(msg[:i])
	}
	return msg
}
