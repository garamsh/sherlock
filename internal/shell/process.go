package shell

import (
	"context"
	"errors"
	"time"
)

// DefaultAgentUID is the default account the agent's shell runs under
// after the platform drops privileges (ADR 0008, ADR 0010). A privileged
// sidecar switches to it; an unprivileged sidecar runs children only when
// the configured exec-uid equals its own euid (single-account mode) — the
// platform binary must otherwise be installed setuid-root, or run under a
// supervisor with the privilege to switch UID.
const DefaultAgentUID = 1001

// ErrAgentUIDUnavailable is returned by ProcessExecutor.Run when the
// child cannot run as AgentUID: AgentUID differs from the euid and the
// process lacks the privilege to switch (typically: not running as root
// or setuid-root), or the host cannot provide the network isolation the
// request needs. Per ADR 0008 the agent falls back to the three typed
// tools in this case.
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
	// (CLONE_NEWNET before the UID drop, or CLONE_NEWUSER|CLONE_NEWNET
	// with a self UID mapping in single-account mode, ADR 0010): no
	// loopback, no interfaces. shell_exec sets it; script tools leave
	// it false — their network is the operator-sanctioned egress
	// channel.
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
