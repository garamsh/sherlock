//go:build linux

package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// rlimits are the resource limits ProcessExecutor applies to the child.
// They are not configurable from the CLI in this iteration (ADR 0012) but
// are kept as struct fields so a future flag can override them.
type rlimits struct {
	CPU          time.Duration
	AddressSpace int64
	FileSize     int64
	OpenFiles    uint64
	Processes    uint64
}

// DefaultRlimits returns the resource caps ProcessExecutor applies. They
// are deliberately tight: a shell call should not consume the whole
// environment.
func DefaultRlimits() rlimits {
	return rlimits{
		CPU:          60 * time.Second,
		AddressSpace: 512 * 1024 * 1024, // 512 MiB virtual memory.
		FileSize:     100 * 1024 * 1024, // 100 MiB per output file.
		OpenFiles:    1024,
		Processes:    64,
	}
}

// ProcessExecutor runs commands via fork+exec+setuid, dropping privileges
// to AgentUID and applying resource limits per ADR 0006 and ADR 0012. It
// is the production Executor implementation.
//
// The zero value is not usable; use NewProcessExecutor.
type ProcessExecutor struct {
	AgentUID       int
	DefaultTimeout time.Duration
	RLimits        rlimits
}

// NewProcessExecutor returns a ProcessExecutor configured with the given
// agent UID and default per-call timeout. It does not validate
// privilege — Run reports ErrAgentUIDUnavailable when the caller cannot
// drop to AgentUID.
func NewProcessExecutor(agentUID int, defaultTimeout time.Duration) *ProcessExecutor {
	return &ProcessExecutor{
		AgentUID:       agentUID,
		DefaultTimeout: defaultTimeout,
		RLimits:        DefaultRlimits(),
	}
}

// Run executes one command and returns its captured output. If the
// caller cannot drop to AgentUID (no root / no setuid-root), Run returns
// ErrAgentUIDUnavailable and a zero Result. On timeout the child is
// SIGKILLed and the returned error is ErrTimedOut with TimedOut set.
//
// Resource limits are applied via prlimit(2) targeting the child PID
// immediately after Start. There is a tiny race window between fork and
// prlimit in which the child could exceed its limits — acceptable for an
// advisory cap on top of the UID-based trust boundary.
func (e *ProcessExecutor) Run(ctx context.Context, req Request) (Result, error) {
	if os.Geteuid() != 0 {
		return Result{}, ErrAgentUIDUnavailable
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = e.DefaultTimeout
	}
	if timeout <= 0 {
		return Result{}, errors.New("shell: timeout is required")
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "sh", "-c", req.Command)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
		// Setuid clears effective + permitted capabilities in the child
		// per capabilities(7); the bounding/inheritable sets are not
		// modified here (Go 1.26 has no SysProcAttr hook to clear them
		// before exec), but the child is no longer root so its caps
		// are inert.
		Credential: &syscall.Credential{Uid: uint32(e.AgentUID), Gid: uint32(e.AgentUID)},
	}
	if req.Workdir != "" {
		cmd.Dir = req.Workdir
	}
	if req.Env != nil {
		cmd.Env = req.Env
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("shell: start: %w", err)
	}

	// Apply resource limits to the now-running child via prlimit(2).
	// Ignore individual rlimit failures — the run still produces a
	// result; we just lack the cap on that one resource.
	e.applyRLimits(cmd.Process.Pid)

	err := cmd.Wait()

	result := Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
		TimedOut: false,
	}
	if err == nil {
		return result, nil
	}

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		result.TimedOut = true
		return result, ErrTimedOut
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			result.ExitCode = ws.ExitStatus()
		} else {
			result.ExitCode = exitErr.ExitCode()
		}
		return result, nil
	}
	return result, fmt.Errorf("shell: %w", err)
}

// applyRLimits applies the configured resource caps to the given PID via
// prlimit(2). A per-resource failure is logged via the kernel and
// ignored: the child still runs, just without that one cap. Setting both
// Cur and Max to the same value produces a hard cap rather than a soft
// one with a higher hard ceiling.
func (e *ProcessExecutor) applyRLimits(pid int) {
	_ = unix.Prlimit(pid, unix.RLIMIT_CPU, cpuLimit(e.RLimits.CPU), nil)
	_ = unix.Prlimit(pid, unix.RLIMIT_AS, &unix.Rlimit{
		Cur: uint64(e.RLimits.AddressSpace),
		Max: uint64(e.RLimits.AddressSpace),
	}, nil)
	_ = unix.Prlimit(pid, unix.RLIMIT_FSIZE, &unix.Rlimit{
		Cur: uint64(e.RLimits.FileSize),
		Max: uint64(e.RLimits.FileSize),
	}, nil)
	_ = unix.Prlimit(pid, unix.RLIMIT_NOFILE, &unix.Rlimit{
		Cur: e.RLimits.OpenFiles,
		Max: e.RLimits.OpenFiles,
	}, nil)
	_ = unix.Prlimit(pid, unix.RLIMIT_NPROC, &unix.Rlimit{
		Cur: e.RLimits.Processes,
		Max: e.RLimits.Processes,
	}, nil)
}

// cpuLimit converts a duration into the seconds unit the kernel expects
// for RLIMIT_CPU. A zero or negative duration produces a 1-second cap
// rather than "unlimited" — the child should never run forever.
func cpuLimit(d time.Duration) *unix.Rlimit {
	seconds := uint64(d / time.Second)
	if d%time.Second != 0 {
		seconds++
	}
	if seconds == 0 {
		seconds = 1
	}
	return &unix.Rlimit{Cur: seconds, Max: seconds}
}
