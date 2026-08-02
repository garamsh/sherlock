package shell

import (
	"context"
	"errors"
	"time"
)

// DefaultAgentUID is the unprivileged UID the agent's shell runs under
// after the platform drops privileges (ADR 0006). The platform binary
// must be installed setuid-root, or run under a supervisor with the
// privilege to switch UID, for ProcessExecutor to use it.
const DefaultAgentUID = 1001

// ErrAgentUIDUnavailable is returned by ProcessExecutor.Run when the
// current effective user cannot drop to AgentUID (typically: not running
// as root or setuid-root). Per ADR 0012 the agent falls back to the three
// typed tools in this case.
var ErrAgentUIDUnavailable = errors.New("shell: agent_uid unavailable")

// ErrTimedOut is returned by ProcessExecutor.Run when the request
// deadline expires before the child exits. The Result's TimedOut flag is
// also set.
var ErrTimedOut = errors.New("shell: timed out")

// Executor runs a shell command and returns its captured output. The
// platform wires ProcessExecutor (fork+exec+setuid) in production and a
// test double in tests; the Tool adapter only knows the interface.
type Executor interface {
	Run(ctx context.Context, req Request) (Result, error)
}

// Request is one command invocation. Timeout <= 0 selects the Executor's
// default. Workdir == "" leaves the child's cwd to the platform default.
// The child starts from the scrubbed environment in tool.ChildEnv; Env
// entries apply on top of it.
type Request struct {
	Command string
	Timeout time.Duration
	Workdir string
	Env     []string
}

// Result is the captured outcome of one command. ExitCode is the wait
// status reported by the kernel; on a non-error exit it is 0 even if the
// command itself returned a non-zero status.
type Result struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	TimedOut bool   `json:"timed_out"`
}
