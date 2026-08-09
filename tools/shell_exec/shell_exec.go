// Package shellexec is the agent's shell_exec tool (ADR 0008): a Go
// package the agent imports and registers as a tool.Tool at compile
// time. Commands run through the agent's native executor
// (internal/shell) in the shared workspace directory, so exec'd
// programs see the same files the files tool serves.
package shellexec

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/garamsh/gagent/internal/shell"
	"github.com/garamsh/gagent/internal/tool"
)

// ToolName is the registered name of the shell_exec tool.
const ToolName = "shell_exec"

// toolDescription is the one-sentence summary advertised to model
// providers, matching the shell_exec responsibility in
// docs/architecture/agent.md (ADR 0008).
const toolDescription = "Run a shell command in the agent's environment and return its captured output. The working directory is the shared workspace directory, so relative paths resolve against the same tree the files tool writes to."

// schemaFile holds the tool's input and output JSON Schemas side by
// side. The conformance test (internal/tool/tooltest) reads the same
// shipped bytes the runtime serves.
//
//go:embed schema.json
var schemaFile []byte

// schemaDoc mirrors schema.json's on-disk shape; the raw messages keep
// the embedded bytes untouched straight through to the LLM-facing
// tool.Tool contract.
type schemaDoc struct {
	Input  json.RawMessage `json:"input"`
	Output json.RawMessage `json:"output"`
}

// schema is the parsed view of schemaFile. A package-level parse keeps
// the embedded bytes a single source of truth and a single failure
// point if the file is malformed.
var schema = func() schemaDoc {
	var doc schemaDoc
	if err := json.Unmarshal(schemaFile, &doc); err != nil {
		panic("shellexec: embedded schema.json is malformed: " + err.Error())
	}
	return doc
}()

// InputSchema returns the JSON Schema for the shell_exec tool's input.
func InputSchema() json.RawMessage { return schema.Input }

// OutputSchema returns the JSON Schema for the shell_exec tool's output.
func OutputSchema() json.RawMessage { return schema.Output }

// Tool is the shell_exec tool adapter: it validates input, delegates to
// the agent's native executor, and marshals the result into the
// tool.Tool contract.
type Tool struct {
	exec           shell.Executor
	workdir        string
	defaultTimeout time.Duration
}

// New returns a shell_exec tool backed by exec. Commands run with
// workdir as the child's working directory — the shared workspace
// directory, the same tree the files tool writes to. defaultTimeout is
// the per-call ceiling when the caller does not pass timeout_seconds
// in the input.
func New(exec shell.Executor, workdir string, defaultTimeout time.Duration) *Tool {
	return &Tool{exec: exec, workdir: workdir, defaultTimeout: defaultTimeout}
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
// the boundary. shell.Result, the output type, is already exported from
// the executor.
type CallInput struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// Call dispatches the command to the executor and marshals the result.
// Input has already passed schema validation at the contract boundary.
// A timeout is a normal outcome, not a tool failure: per ADR 0008 the
// model receives the structured result with timed_out: true.
func (t *Tool) Call(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	return tool.CallTyped(ctx, t.doCall, input)
}

func (t *Tool) doCall(ctx context.Context, args CallInput) (shell.Result, error) {
	if args.Command == "" {
		return shell.Result{}, errors.New("command is required")
	}
	timeout := t.defaultTimeout
	if args.TimeoutSeconds > 0 {
		timeout = time.Duration(args.TimeoutSeconds) * time.Second
	}
	res, err := t.exec.Run(ctx, shell.Request{
		Command:   args.Command,
		Timeout:   timeout,
		Workdir:   t.workdir,
		NoNetwork: true,
	})
	if errors.Is(err, shell.ErrTimedOut) {
		res.TimedOut = true
		return res, nil
	}
	return res, err
}
