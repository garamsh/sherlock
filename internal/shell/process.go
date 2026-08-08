package shell

import (
	"context"
	"errors"
	"time"
)

// DefaultAgentUID is the unprivileged UID the agent's shell runs under
// after the platform drops privileges (ADR 0008, ADR 0010). The platform binary
// must be installed setuid-root, or run under a supervisor with the
// privilege to switch UID, for ProcessExecutor to use it.
const DefaultAgentUID = 1001

// DefaultMemoryUID is the UID that owns the environment's memory.db in a
// privileged deployment (ADR 0008, ADR 0010): the platform chowns the file to it at
// environment creation, so agent-UID children get EACCES from the kernel
// and memory access goes only through the memory service.
const DefaultMemoryUID = 1002

// ErrAgentUIDUnavailable is returned by ProcessExecutor.Run when the
// current effective user cannot drop to AgentUID (typically: not running
// as root or setuid-root). Per ADR 0008 the agent falls back to the three
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
	// Command is the program to run. With no Args it is interpreted by
	// `sh -c`; with Args it is the executable and the Args reach it
	// verbatim (the script-tool argv form, ADR 0009).
	Command string
	Args    []string
	// Stdin, when non-nil, is fed to the child's standard input.
	Stdin   []byte
	Timeout time.Duration
	Workdir string
	Env     []string
	// NoNetwork runs the child in a fresh, empty network namespace
	// (CLONE_NEWNET before the UID drop, ADR 0010): no loopback, no
	// interfaces. shell_exec sets it; script tools leave it false —
	// their network is the operator-sanctioned egress channel.
	NoNetwork bool
}

// Result is the captured outcome of one command. ExitCode is the
// command's exit status; a non-zero exit is a normal outcome, not an
// error.
type Result struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	TimedOut bool   `json:"timed_out"`
}

// unavailableExecutor reports ErrAgentUIDUnavailable on every Run.
type unavailableExecutor struct{}

func (unavailableExecutor) Run(context.Context, Request) (Result, error) {
	return Result{}, ErrAgentUIDUnavailable
}

// UnavailableExecutor returns an Executor whose Run always fails with
// ErrAgentUIDUnavailable. It backs script tools when the platform wires
// no executor: registration still succeeds but calls fail closed instead
// of silently running as the platform UID (ADR 0010).
func UnavailableExecutor() Executor { return unavailableExecutor{} }
