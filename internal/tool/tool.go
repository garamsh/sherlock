package tool

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Manifest is the contract the platform reads from tools/<name>/manifest.json
// (ADR 0009 §Decision). The manifest is the full description the platform
// trusts: name, description, command, env, and the two JSON Schemas. The
// descriptor the model sees is a strict projection of this struct.
type Manifest struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Command      string          `json:"command"`
	Env          []string        `json:"env,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema"`
}

// Descriptor is the model-facing view of a tool: its name, description,
// and JSON Schema documents, JSON-serializable so it can be handed to a
// model. The descriptor is a strict projection of the on-disk manifest
// (ADR 0009 §Decision): the runner's command, env, and on-disk paths
// never reach the model. Hash is the descriptor's content hash; the
// platform pins it against any drift on the next startup.
type Descriptor struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema"`
	Hash         string          `json:"hash"`
}

// ManifestPins is the operator's accepted tool set: the SHA-256 of the
// canonical manifest bytes for each tool the operator reviewed and
// accepted. A tool whose manifest hash drifts from its pin is rejected
// at startup, and so is a tool the set does not name — the set states
// which tools are accepted, so an unnamed one is unreviewed. An empty
// non-nil set therefore accepts no tool at all.
//
// The pin is operator material: only the operator can say which
// manifest they accepted, so the platform reads a pin set and never
// manufactures one (docs/architecture/security.md §Tool trust posture:
// free contribution, operator installation).
type ManifestPins map[string]string

// ValidationError reports input that failed schema validation at the
// contract boundary. It matches ErrValidation via errors.Is. The
// description restates the tool's advertised summary so a model reading
// the rejection is steered back to the tool's real contract.
type ValidationError struct {
	Tool        string
	Description string
	Err         error
}

// Error returns a human-readable description of the validation failure.
func (e *ValidationError) Error() string {
	if e.Description == "" {
		return fmt.Sprintf("tool %q: %v", e.Tool, e.Err)
	}
	return fmt.Sprintf("tool %q: %v (tool description: %s)", e.Tool, e.Err, e.Description)
}

// Unwrap returns the underlying validation failure.
func (e *ValidationError) Unwrap() error { return e.Err }

// Is reports whether target is ErrValidation.
func (e *ValidationError) Is(target error) bool { return target == ErrValidation }

// ExecutionError reports a failure inside the tool body after the input
// passed validation. It matches ErrExecution via errors.Is.
type ExecutionError struct {
	Tool string
	Err  error
}

// Error returns a human-readable description of the execution failure.
func (e *ExecutionError) Error() string {
	return fmt.Sprintf("tool %q: %v", e.Tool, e.Err)
}

// Unwrap returns the underlying execution failure.
func (e *ExecutionError) Unwrap() error { return e.Err }

// Is reports whether target is ErrExecution.
func (e *ExecutionError) Is(target error) bool { return target == ErrExecution }

// Sentinel errors at the boundary. Validation fires before the tool
// body; execution fires after.
var (
	ErrValidation = errors.New("tool input validation failed")
	ErrExecution  = errors.New("tool execution failed")
)
