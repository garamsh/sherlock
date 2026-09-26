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
const memoryWriteDescription = "Deliver your reply and your whole note. " +
	"Every turn ends with this call and no turn ends without it. The reply is what the person reads; " +
	"the note is restated in full every turn. The runtime saves visible conversation separately."

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

// MaxNoteLength bounds the note in characters. A note that outgrows it
// is refused, so the model consolidates it instead of the runtime
// cutting it.
const MaxNoteLength = 2000

// ResponseSchema is the JSON Schema the terminal call is constrained to.
// It is the reserved tool's parameters, and ParseWriteback reads what the
// model puts in them, so the schema and the parser are one contract and
// are kept together.
//
// The note is a required string the model restates whole every turn, so
// keeping an instruction is an act the model takes each time. A nullable
// note that meant "unchanged" was left null on every turn by MiniMax-M2,
// even after a standing instruction (issue #862's pilot).
const ResponseSchema = `{
  "type": "object",
  "properties": {
    "reply": {
      "type": "string",
      "description": "Everything the person reads. The note is not shown to them."
    },
    "note": {
      "type": "string",
      "maxLength": 2000,
      "description": "Your whole note as it stands after this turn: every standing instruction and preference the person has given, and the current work with its goal, constraints, status and next step. Copy it unchanged when nothing in it changed; add what this turn stated."
    }
  },
  "required": ["reply", "note"],
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

// writebackJSON is the wire shape of the terminal call. Note is a pointer
// so a call that leaves it out, or sends null, is told apart from one that
// clears it.
type writebackJSON struct {
	Reply string  `json:"reply"`
	Note  *string `json:"note"`
}

// ParseWriteback splits the terminal call's arguments into the visible
// reply and the note. A note the call leaves out or sends as null keeps
// the saved one. Arguments that are not JSON fail with *WritebackError,
// and a note longer than MaxNoteLength with ErrNoteTooLong, which the
// session hands back to the model rather than failing the turn.
func ParseWriteback(content string) (reply string, note *string, err error) {
	var doc writebackJSON
	if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(content)), &doc); jsonErr != nil {
		return "", nil, &WritebackError{Err: fmt.Errorf("invalid JSON: %s", jsonErrorMessage(jsonErr))}
	}
	if doc.Note != nil {
		trimmed := strings.TrimSpace(*doc.Note)
		if n := len([]rune(trimmed)); n > MaxNoteLength {
			return "", nil, fmt.Errorf("%w: %d characters, at most %d", ErrNoteTooLong, n, MaxNoteLength)
		}
		doc.Note = &trimmed
	}
	return strings.TrimSpace(doc.Reply), doc.Note, nil
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
