package script

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/garamsh/gagent/internal/shell"
	"github.com/garamsh/gagent/internal/tool"
)

// DefaultTimeout is the per-call ceiling when the spec does not set
// exec.timeout.
const DefaultTimeout = 30 * time.Second

// Spec is the declarative tool specification: name, description, schemas,
// and the execution command. The platform registers the tool from the
// spec; the spec itself is the contract.
type Spec struct {
	Name         string
	Description  string
	InputSchema  json.RawMessage
	OutputSchema json.RawMessage
	Exec         ExecSpec
}

// ExecSpec describes how to run the tool: the argv array (first element is
// the executable, the rest are arguments), the stdin/stdout formats, the
// per-call timeout, and an optional environment and working directory.
type ExecSpec struct {
	Command      []string
	StdinFormat  string
	StdoutFormat string
	Timeout      time.Duration
	Env          []string
	Workdir      string
}

// ToolScript is the universal adapter: it runs any executable with JSON
// input on stdin and returns JSON output on stdout. Execution goes
// through the shell.Executor, which drops the child to the agent UID
// (ADR 0014); the child's network is not namespaced — a script tool is
// an operator-sanctioned egress channel. When the platform cannot drop
// privileges, Call fails with the executor's agent_uid unavailable error
// instead of silently running as the platform UID.
type ToolScript struct {
	spec Spec
	exec shell.Executor
}

var _ tool.Tool = (*ToolScript)(nil)

// New returns a ToolScript for the given spec, executing through exec.
func New(spec Spec, exec shell.Executor) *ToolScript {
	return &ToolScript{spec: spec, exec: exec}
}

// Name returns the tool name from the spec.
func (t *ToolScript) Name() string { return t.spec.Name }

// Description returns the one-sentence tool summary from the spec.
func (t *ToolScript) Description() string { return t.spec.Description }

// InputSchema returns the JSON Schema for the tool input.
func (t *ToolScript) InputSchema() json.RawMessage { return t.spec.InputSchema }

// OutputSchema returns the JSON Schema for the tool output.
func (t *ToolScript) OutputSchema() json.RawMessage { return t.spec.OutputSchema }

// Call runs the spec's command with input on stdin and returns stdout.
// When exec.stdout_format is "json", stdout is validated and returned as a
// JSON document; otherwise it is returned as-is.
func (t *ToolScript) Call(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	if len(t.spec.Exec.Command) == 0 {
		return nil, fmt.Errorf("exec.command is required")
	}
	timeout := t.spec.Exec.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	res, err := t.exec.Run(ctx, shell.Request{
		Command: t.spec.Exec.Command[0],
		Args:    t.spec.Exec.Command[1:],
		Stdin:   input,
		Timeout: timeout,
		Workdir: t.spec.Exec.Workdir,
		Env:     t.spec.Exec.Env,
	})
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("exit code %d: %s", res.ExitCode, res.Stderr)
	}
	if t.spec.Exec.StdoutFormat == "json" {
		var out json.RawMessage
		if err := json.Unmarshal([]byte(res.Stdout), &out); err != nil {
			return nil, fmt.Errorf("invalid JSON output: %w", err)
		}
		return out, nil
	}
	return []byte(res.Stdout), nil
}
