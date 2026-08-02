package script

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

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
// input on stdin and returns JSON output on stdout.
type ToolScript struct {
	spec Spec
}

var _ tool.Tool = (*ToolScript)(nil)

// New returns a ToolScript for the given spec.
func New(spec Spec) *ToolScript { return &ToolScript{spec: spec} }

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
		return nil, fmt.Errorf("tool %q: exec.command is required", t.spec.Name)
	}
	timeout := t.spec.Exec.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, t.spec.Exec.Command[0], t.spec.Exec.Command[1:]...)
	if t.spec.Exec.Workdir != "" {
		cmd.Dir = t.spec.Exec.Workdir
	}
	cmd.Env = append(os.Environ(), t.spec.Exec.Env...)
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("tool %q: %w: %s", t.spec.Name, err, stderr.String())
	}
	if t.spec.Exec.StdoutFormat == "json" {
		var out json.RawMessage
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			return nil, fmt.Errorf("tool %q: invalid JSON output: %w", t.spec.Name, err)
		}
		return out, nil
	}
	return stdout.Bytes(), nil
}
