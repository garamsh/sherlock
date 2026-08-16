package shell

import (
	"context"
	"errors"
	"time"
)

// DefaultAgentUID is the default account the workspace's exec children
// run under after the platform drops privileges (ADR 0008, ADR 0010). A
// privileged workspace switches to it; an unprivileged workspace runs
// children only when the configured exec-uid equals its own euid
// (single-account mode) — the platform binary must otherwise be
// installed setuid-root, or run under a supervisor with the privilege
// to switch UID.
const DefaultAgentUID = 1001

// ErrAgentUIDUnavailable is returned by ProcessExecutor.Run when the
// child cannot run as AgentUID: AgentUID differs from the euid and the
// process lacks the privilege to switch (typically: not running as root
// or setuid-root), or the host cannot provide the network isolation the
// request needs.
var ErrAgentUIDUnavailable = errors.New("shell: agent_uid unavailable")

// ErrRlimitsUnavailable is returned by ProcessExecutor.Run when an
// untrusted child's resource caps cannot be applied. The caps are part
// of the boundary around agent-influenced code (ADR 0008, ADR 0010), so
// the call fails and no command runs — the executor never downgrades to
// an uncapped child.
var ErrRlimitsUnavailable = errors.New("shell: resource limits unavailable")

// ErrTimedOut is returned by ProcessExecutor.Run when the request
// deadline expires before the child exits. The Result's TimedOut flag is
// also set.
var ErrTimedOut = errors.New("shell: timed out")

// Executor runs a shell command and returns its captured output. The
// platform wires ProcessExecutor (fork+exec+setuid) in production and a
// test double in tests; the workspace gRPC server only knows the
// interface.
type Executor interface {
	Run(ctx context.Context, req Request) (Result, error)
}

// Request is one command invocation. Timeout <= 0 selects the Executor's
// default. Workdir == "" leaves the child's cwd to the platform default.
// The child starts from the scrubbed environment in childEnv; Env
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
	// loopback, no interfaces. Tools that need a network-isolated
	// child set it; tools whose network is the operator-sanctioned
	// egress channel leave it false.
	NoNetwork bool
	// Trusted names the child's trust class (ADR 0010): resource caps
	// bind untrusted code — agent-supplied commands carry the
	// executor's rlimits — while operator-sanctioned children run
	// without them, bounded by their per-call timeout and the
	// deployment's container/pod limits.
	Trusted bool
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
