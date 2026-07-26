package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

const execInputSchema = `{
	"type": "object",
	"properties": {
		"command": {"type": "string", "minLength": 1}
	},
	"required": ["command"],
	"additionalProperties": false
}`

const execOutputSchema = `{
	"type": "object",
	"properties": {
		"stdout": {"type": "string"},
		"stderr": {"type": "string"},
		"exit_code": {"type": "integer"},
		"timed_out": {"type": "boolean"}
	},
	"required": ["stdout", "stderr", "exit_code", "timed_out"],
	"additionalProperties": false
}`

// ExecToolName is the registered name of the sandbox_exec tool.
const ExecToolName = "sandbox_exec"

// ExecTool is the sandbox_exec tool: it runs agent-supplied shell commands
// inside the environment's container. The container is created lazily on
// the first call and lives until Destroy. ExecTool is safe for concurrent
// use.
type ExecTool struct {
	rt      Runtime
	cfg     Config
	timeout time.Duration

	mu          sync.Mutex
	containerID string
}

// NewExecTool returns a sandbox_exec tool for one environment. timeout
// bounds every command execution.
func NewExecTool(rt Runtime, cfg Config, timeout time.Duration) *ExecTool {
	return &ExecTool{rt: rt, cfg: cfg, timeout: timeout}
}

// Name returns the tool name.
func (t *ExecTool) Name() string { return ExecToolName }

// InputSchema returns the JSON Schema for the tool input.
func (t *ExecTool) InputSchema() json.RawMessage { return json.RawMessage(execInputSchema) }

// OutputSchema returns the JSON Schema for the tool result.
func (t *ExecTool) OutputSchema() json.RawMessage { return json.RawMessage(execOutputSchema) }

// Call runs the command under /bin/sh -c inside the environment container
// and returns its captured output. Input has already passed schema
// validation at the contract boundary.
func (t *ExecTool) Call(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, fmt.Errorf("decode input: %w", err)
	}
	id, err := t.ensureContainer(ctx)
	if err != nil {
		return nil, err
	}
	result, err := t.rt.Exec(ctx, id, []string{"/bin/sh", "-c", args.Command}, t.timeout)
	if err != nil {
		return nil, fmt.Errorf("exec command: %w", err)
	}
	out, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode result: %w", err)
	}
	return out, nil
}

// Destroy removes the environment container. It is idempotent.
func (t *ExecTool) Destroy(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.containerID == "" {
		return nil
	}
	if err := t.rt.Destroy(ctx, t.containerID); err != nil {
		return fmt.Errorf("destroy container: %w", err)
	}
	t.containerID = ""
	return nil
}

// ensureContainer creates the environment container on first use.
func (t *ExecTool) ensureContainer(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.containerID != "" {
		return t.containerID, nil
	}
	id, err := t.rt.Create(ctx, t.cfg)
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}
	t.containerID = id
	return id, nil
}
