package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/garamsh/gagent/internal/tool"
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
const memoryWriteDescription = "Deliver your reply and record what this turn stated. " +
	"Every turn ends with this call and no turn ends without it: the reply you pass here " +
	"is what the person reads, and memory_ops is the list of facts the turn stated — " +
	"the empty list is your answer that it stated none."

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

// ResponseSchema is the JSON Schema the writeback is constrained to
// (ADR 0018). It is the reserved tool's parameters, and ParseWriteback
// reads what the model puts in them, so the schema and the parser are
// one contract and are kept together.
//
// Three properties carry the decision. The memory field is required, so
// a response has no shape in which it is absent and an empty list is an
// answer the model states rather than a field it omits. The address is
// the subject and the question the fact answers — not the subject and a
// topic noun — with the restriction the sentence puts on that question.
// The operation is nullable, because a statement that narrows its own
// question gets its own address and declares nothing.
const ResponseSchema = `{
  "type": "object",
  "properties": {
    "reply": {
      "type": "string",
      "description": "Everything the person reads. The memory field is not shown to them."
    },
    "memory_ops": {
      "type": "array",
      "description": "One entry per fact this turn states. The empty list is an answer, not an omission.",
      "items": {
        "type": "object",
        "properties": {
          "address": {
            "type": "object",
            "description": "Where the fact is held: the question it answers, not the topic it is about.",
            "properties": {
              "subject": {
                "type": "string",
                "description": "Who or what the fact is about."
              },
              "question": {
                "type": "string",
                "description": "The question this fact answers, in the statement's own words."
              },
              "restriction": {
                "type": ["string", "null"],
                "description": "What the statement narrows that question to, in its own words. Null where it narrows nothing."
              }
            },
            "required": ["subject", "question", "restriction"],
            "additionalProperties": false
          },
          "value": {
            "type": "string",
            "description": "The answer to that question, stated on its own."
          },
          "operation": {
            "type": ["string", "null"],
            "enum": ["replace", "join", null],
            "description": "replace where the value ends what that address holds, join where it stands beside it. Null where the statement answers a question of its own."
          }
        },
        "required": ["address", "value", "operation"],
        "additionalProperties": false
      }
    }
  },
  "required": ["reply", "memory_ops"],
  "additionalProperties": false
}`

// WritebackError reports a memory write that could not be applied. It
// matches ErrMalformedWriteback via errors.Is. Parse failures — a
// response that is not the schema's document — are returned as warnings:
// the visible reply still reaches the user and the turn's memory update
// is dropped (issue #93). Schema violations (a write with no address or
// no value) are fatal: the platform refuses to apply a
// contract-violating writeback.
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

// writebackJSON is the wire shape of a model response: the visible reply
// and the memory writes the turn states. Restriction is a pointer
// because a statement that narrows nothing states null; Operation is
// left undecoded because no JSON value in it may fail the document —
// whatever it holds that is not a readable declaration joins.
type writebackJSON struct {
	Reply string `json:"reply"`
	Ops   []struct {
		Address struct {
			Subject     string  `json:"subject"`
			Question    string  `json:"question"`
			Restriction *string `json:"restriction"`
		} `json:"address"`
		Value     string          `json:"value"`
		Operation json.RawMessage `json:"operation"`
	} `json:"memory_ops"`
}

// ParseWriteback splits a model response into its visible reply and the
// memory writes it states. A response with no content at all — the step
// that only requests tool calls — states nothing and returns no
// operations; a response that parses returns a non-nil, possibly empty
// ops slice, so the event stream can tell an empty writeback from a
// missing one (issue #193). Failures are reported as *WritebackError:
// parse failures as warn, schema violations as the fatal err.
func ParseWriteback(content string) (visible string, ops []WritebackOp, warn error, err error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", nil, nil, nil
	}
	var doc writebackJSON
	if jsonErr := json.Unmarshal([]byte(trimmed), &doc); jsonErr != nil {
		parseWarn := &WritebackError{Err: fmt.Errorf("invalid JSON: %s", jsonErrorMessage(jsonErr))}
		slog.Warn("agent: writeback dropped", "op", "parse_writeback", "reason", parseWarn.Error())
		return trimmed, nil, parseWarn, nil
	}
	ops, err = doc.ops()
	if err != nil {
		return "", nil, nil, err
	}
	return strings.TrimSpace(doc.Reply), ops, nil, nil
}

// ops turns the decoded response into the writes the sink applies. An
// address missing its subject or its question cannot be landed on and a
// write with no value holds nothing, so both are contract violations.
func (b *writebackJSON) ops() ([]WritebackOp, error) {
	ops := make([]WritebackOp, 0, len(b.Ops))
	for _, o := range b.Ops {
		subject := strings.TrimSpace(o.Address.Subject)
		question := strings.TrimSpace(o.Address.Question)
		value := strings.TrimSpace(o.Value)
		if subject == "" || question == "" || value == "" {
			return nil, &WritebackError{
				Err: errors.New("memory op requires a subject, a question and a value"),
			}
		}
		ops = append(ops, WritebackOp{
			Address: MemoryAddress{
				Subject:     subject,
				Question:    question,
				Restriction: trimPointer(o.Address.Restriction),
			},
			Value:     value,
			Operation: parseOperation(o.Operation),
		})
	}
	return ops, nil
}

// parseOperation reads the operation a write declares. Only "replace"
// destroys; every other answer — the field absent, null, empty, a word
// the contract does not name, or a JSON value that is not a string at
// all — joins (ADR 0018 §Where a writeback must err). Nothing here
// fails: a declaration the parser cannot read is the case the join
// default exists for. What was stated and could not be read is logged,
// because the join it falls back to is otherwise indistinguishable from
// a declared one.
func parseOperation(declared json.RawMessage) MemoryOperation {
	if len(declared) == 0 {
		return OpJoin
	}
	var word string
	if err := json.Unmarshal(declared, &word); err != nil {
		slog.Warn("agent: writeback operation unreadable, joined",
			"op", "parse_writeback", "declared", string(declared))
		return OpJoin
	}
	switch strings.ToLower(strings.TrimSpace(word)) {
	case string(OpReplace):
		return OpReplace
	case string(OpJoin), "":
		return OpJoin
	default:
		slog.Warn("agent: writeback operation unreadable, joined",
			"op", "parse_writeback", "declared", word)
		return OpJoin
	}
}

// trimPointer returns the trimmed string a pointer holds, or the empty
// string when it holds none.
func trimPointer(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
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
