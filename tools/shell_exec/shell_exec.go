// Package shellexec is the agent's shell_exec tool (ADR 0008): a Go
// package the agent imports and registers as a tool.Tool at compile
// time. Commands cross the wire to the workspace's Exec RPC
// (internal/workspace/client) — the workspace owns the executor and
// runs children in its own directory, so exec'd programs see the same
// files the files tool serves. The agent executes nothing itself.
package shellexec

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/garamsh/gagent/internal/tool"
	"github.com/garamsh/gagent/internal/workspace"
	"github.com/garamsh/gagent/internal/workspace/shell"
)

// ToolName is the registered name of the shell_exec tool.
const ToolName = "shell_exec"

// toolDescription is the one-sentence summary advertised to model
// providers, matching the shell_exec responsibility in
// docs/architecture/agent.md (ADR 0008).
const toolDescription = "Run a shell command in the agent's workspace and return its captured output. The working directory is the workspace directory, so relative paths resolve against the same tree the files tool writes to."

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

// Tool is the shell_exec tool adapter: it validates input, calls the
// workspace's Exec RPC through the typed workspace client, and marshals
// the result into the tool.Tool contract.
type Tool struct {
	ws workspace.WorkspaceClient
}

// New returns a shell_exec tool backed by the workspace client ws.
// Commands run in the workspace directory — the same tree the files
// tool serves — under the trust story the workspace's executor owns
// (drop to the exec UID, rlimits, network namespace; ADR 0008,
// ADR 0010). The per-call timeout ceiling is the workspace's
// exec-timeout unless the caller passes timeout_seconds in the input.
func New(ws workspace.WorkspaceClient) *Tool {
	return &Tool{ws: ws}
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
// the workspace's executor package.
type CallInput struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// Call dispatches the command to the workspace's Exec RPC and marshals
// the result. Input has already passed schema validation at the
// contract boundary. A timeout is a normal outcome, not a tool failure:
// per ADR 0008 the model receives the structured result with
// timed_out: true.
func (t *Tool) Call(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	return tool.CallTyped(ctx, t.doCall, input)
}

func (t *Tool) doCall(ctx context.Context, args CallInput) (shell.Result, error) {
	if args.Command == "" {
		return shell.Result{}, errors.New("command is required")
	}
	req := &workspace.ExecRequest{
		Command:   args.Command,
		NoNetwork: true,
	}
	if args.TimeoutSeconds > 0 {
		req.TimeoutNanos = int64(time.Duration(args.TimeoutSeconds) * time.Second)
	}
	resp, err := t.ws.Exec(ctx, req)
	if err != nil {
		return shell.Result{}, err
	}
	return shell.Result{
		Stdout:   resp.GetStdout(),
		Stderr:   resp.GetStderr(),
		ExitCode: int(resp.GetExitCode()),
		TimedOut: resp.GetTimedOut(),
	}, nil
}
