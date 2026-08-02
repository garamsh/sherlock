package shell

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/garamsh/gagent/internal/tool"
)

// ToolName is the registered name of the shell_exec tool.
const ToolName = "shell_exec"

// toolDescription is the one-sentence summary advertised to model
// providers, matching the shell_exec responsibility in
// docs/architecture/agent.md (ADR 0012).
const toolDescription = "Run a shell command in the agent's environment and return its captured output."

// Tool is the shell_exec tool adapter: it validates input, delegates to an
// Executor, and marshals the result into the tool.Tool contract.
type Tool struct {
	exec           Executor
	defaultTimeout time.Duration
}

// NewTool returns a shell_exec tool backed by exec. The defaultTimeout is
// the per-call ceiling when the caller does not pass timeout_seconds in
// the input.
func NewTool(exec Executor) *Tool {
	return &Tool{exec: exec, defaultTimeout: 30 * time.Second}
}

// WithDefaultTimeout overrides the tool's default per-call timeout. A
// value <= 0 keeps the currently configured timeout.
func (t *Tool) WithDefaultTimeout(d time.Duration) *Tool {
	if d > 0 {
		t.defaultTimeout = d
	}
	return t
}

// Name returns the tool name.
func (t *Tool) Name() string { return ToolName }

// Description returns the one-sentence tool summary.
func (t *Tool) Description() string { return toolDescription }

// InputSchema returns the JSON Schema for the tool input.
func (t *Tool) InputSchema() json.RawMessage { return InputSchema() }

// OutputSchema returns the JSON Schema for the tool output.
func (t *Tool) OutputSchema() json.RawMessage { return OutputSchema() }

// CallInput is the canonical typed form of schema.json's input shape;
// the schema/struct consistency test keeps them in lockstep. Only the
// fields the runtime honours are decoded; the schema's
// additionalProperties:false gate means anything else was rejected at
// the boundary. Result, the output type, is already exported from
// process.go.
type CallInput struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// Call dispatches the command to the executor and marshals the result.
// Input has already passed schema validation at the contract boundary.
// A timeout is a normal outcome, not a tool failure: per ADR 0012 the
// model receives the structured result with timed_out: true.
func (t *Tool) Call(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	return tool.CallTyped(ctx, t.doCall, input)
}

func (t *Tool) doCall(ctx context.Context, args CallInput) (Result, error) {
	if args.Command == "" {
		return Result{}, errors.New("command is required")
	}
	timeout := t.defaultTimeout
	if args.TimeoutSeconds > 0 {
		timeout = time.Duration(args.TimeoutSeconds) * time.Second
	}
	res, err := t.exec.Run(ctx, Request{
		Command:   args.Command,
		Timeout:   timeout,
		NoNetwork: true,
	})
	if errors.Is(err, ErrTimedOut) {
		res.TimedOut = true
		return res, nil
	}
	return res, err
}
