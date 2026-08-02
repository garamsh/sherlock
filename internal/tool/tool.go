package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

var (
	// ErrValidation marks errors produced by input validation at the
	// contract boundary, before the tool body runs.
	ErrValidation = errors.New("tool input validation failed")
	// ErrExecution marks errors produced by the tool body itself, after
	// the input has passed validation.
	ErrExecution = errors.New("tool execution failed")
)

// Tool is the contract every tool implements. Schemas are JSON Schema
// documents; they must stay JSON-serializable so they can be handed to a
// model. Call receives input already validated against InputSchema and
// returns a JSON-serializable structured result.
type Tool interface {
	// Name returns the unique tool name used for registration and lookup.
	Name() string
	// Description returns a one-sentence human/model-readable summary of
	// what the tool does; it is advertised to model providers.
	Description() string
	// InputSchema returns the JSON Schema document describing valid input.
	InputSchema() json.RawMessage
	// OutputSchema returns the JSON Schema document describing the result.
	OutputSchema() json.RawMessage
	// Call executes the tool with validated input.
	Call(ctx context.Context, input json.RawMessage) (json.RawMessage, error)
}

// ValidationError reports input that failed schema validation at the
// contract boundary. It matches ErrValidation via errors.Is. Description
// restates the tool's advertised summary inside the error text so a model
// reading the rejection is steered back to the tool's real contract
// (issue #160: write-shaped memory calls must redirect to the writeback
// block, not invite argument variants).
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

// Invoke is the contract boundary for calling a tool: it validates input
// against the tool's input schema before the tool body runs, then delegates
// to Call. Validation failures are returned as *ValidationError; failures
// from the tool body are returned as *ExecutionError.
func Invoke(ctx context.Context, t Tool, input json.RawMessage) (json.RawMessage, error) {
	if err := validate(t.InputSchema(), input); err != nil {
		return nil, &ValidationError{Tool: t.Name(), Description: t.Description(), Err: err}
	}
	out, err := t.Call(ctx, input)
	if err != nil {
		return nil, &ExecutionError{Tool: t.Name(), Err: err}
	}
	return out, nil
}
