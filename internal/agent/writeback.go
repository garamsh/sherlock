package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// ErrMalformedWriteback marks a model response whose memory block fails
// the writeback contract: a JSON parse error or a schema violation. It is
// surfaced as a fatal error only for schema violations (the model emitted
// a contract the platform refuses to apply). JSON parse failures — the
// fence was malformed or the JSON inside was unparseable — are recorded
// as warnings, not fatal errors, so a single bad writeback does not
// abort an otherwise-successful turn (issue #93).
var ErrMalformedWriteback = errors.New("malformed memory writeback block")

// writebackFence opens the structured memory block that ends every model
// response (ADR 0006 step 4).
const writebackFence = "```memory"

// WritebackError reports a memory block that could not be applied. It
// matches ErrMalformedWriteback via errors.Is. Parse failures — an
// unterminated fence or invalid JSON — are returned as warnings: the
// visible reply still reaches the user and the turn's memory update is
// dropped (issue #93). Schema violations (e.g. keep with empty id) are
// fatal: the platform refuses to apply a contract-violating writeback.
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

// writebackJSON is the wire shape of the memory block: keep/update/delete
// reference recalled memories by ID; add carries new facts.
type writebackJSON struct {
	Keep   []string `json:"keep"`
	Update []struct {
		ID      string `json:"id"`
		Content string `json:"content"`
	} `json:"update"`
	Delete []string `json:"delete"`
	Add    []struct {
		Content string `json:"content"`
		Type    string `json:"type"`
	} `json:"add"`
}

// ParseWriteback splits a model response into its visible content and
// the memory operations of its trailing writeback block. With no fence
// it returns the whole content with no operations; a present but empty
// block returns a non-nil, empty ops slice so the event stream can tell
// an empty writeback from a missing one (issue #193). Failures are
// reported as *WritebackError: parse failures as warn, schema violations
// as the fatal err.
func ParseWriteback(content string) (visible string, ops []WritebackOp, warn error, err error) {
	idx := strings.Index(content, writebackFence)
	if idx < 0 {
		return content, nil, nil, nil
	}
	visible = strings.TrimSpace(content[:idx])
	rest := content[idx+len(writebackFence):]
	end := strings.Index(rest, "```")
	if end < 0 {
		parseWarn := &WritebackError{Err: errors.New("unterminated ```memory fence")}
		slog.Warn("agent: writeback dropped", "op", "parse_writeback", "reason", parseWarn.Error())
		return visible, nil, parseWarn, nil
	}
	var block writebackJSON
	if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(rest[:end])), &block); jsonErr != nil {
		parseWarn := &WritebackError{Err: fmt.Errorf("invalid JSON: %s", jsonErrorMessage(jsonErr))}
		slog.Warn("agent: writeback dropped", "op", "parse_writeback", "reason", parseWarn.Error())
		return visible, nil, parseWarn, nil
	}
	ops, err = block.ops()
	if err != nil {
		return "", nil, nil, err
	}
	if ops == nil {
		ops = []WritebackOp{}
	}
	return visible, ops, nil, nil
}

func (b *writebackJSON) ops() ([]WritebackOp, error) {
	var ops []WritebackOp
	for _, id := range b.Keep {
		if id == "" {
			return nil, &WritebackError{Err: errors.New("keep operation missing id")}
		}
		ops = append(ops, WritebackOp{Kind: OpKeep, ID: id})
	}
	for _, u := range b.Update {
		if u.ID == "" || u.Content == "" {
			return nil, &WritebackError{Err: errors.New("update operation requires id and content")}
		}
		ops = append(ops, WritebackOp{Kind: OpUpdate, ID: u.ID, Content: u.Content})
	}
	for _, id := range b.Delete {
		if id == "" {
			return nil, &WritebackError{Err: errors.New("delete operation missing id")}
		}
		ops = append(ops, WritebackOp{Kind: OpDelete, ID: id})
	}
	for _, a := range b.Add {
		if a.Content == "" {
			return nil, &WritebackError{Err: errors.New("add operation missing content")}
		}
		ops = append(ops, WritebackOp{Kind: OpAdd, Content: a.Content, Type: a.Type})
	}
	return ops, nil
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
