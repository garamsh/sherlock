//go:build linux

package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/garamsh/gagent/internal/tool"
)

// rlimits are the resource limits ProcessExecutor applies to the child.
// They are not configurable from the CLI (ADR 0008) but are kept as
// struct fields so a flag can override them.
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
// to AgentUID and applying resource limits per ADR 0008 and ADR 0010. It
// is the production Executor implementation.
//
// When AgentUID equals the process's euid the deployment declares a
// single-account computer (ADR 0008): no UID switch is needed and no
// privilege is required — children run as the sidecar's own account.
//
// The zero value is not usable; use NewProcessExecutor.
type ProcessExecutor struct {
	AgentUID       int
	DefaultTimeout time.Duration
	RLimits        rlimits
	// netNS is true when this process can create a fresh network
	// namespace (euid 0 with CAP_SYS_ADMIN effective). A root container
	// without CAP_SYS_ADMIN is not, and the netns unshare in Start
	// would surface as a raw fork/exec EPERM. NewProcessExecutor
	// detects it once at construction; tests set it directly.
	netNS bool
}

// NewProcessExecutor returns a ProcessExecutor configured with the given
// agent UID and default per-call timeout. It does not validate
// privilege — Run reports ErrAgentUIDUnavailable when the caller cannot
// drop to AgentUID, including when it cannot create the fresh network
// namespace required for NoNetwork.
func NewProcessExecutor(agentUID int, defaultTimeout time.Duration) *ProcessExecutor {
	exec := &ProcessExecutor{
		AgentUID:       agentUID,
		DefaultTimeout: defaultTimeout,
		RLimits:        DefaultRlimits(),
		netNS:          netNSCapable(),
	}
	if !exec.netNS && os.Geteuid() == 0 {
		slog.Warn("shell: CAP_SYS_ADMIN missing; shell_exec will report agent_uid unavailable",
			"reason", "unshare(CLONE_NEWNET) requires CAP_SYS_ADMIN")
	}
	return exec
}

// Run executes one command and returns its captured output. If the
// caller cannot run the child as AgentUID — AgentUID differs from the
// euid and the process is not root — Run returns ErrAgentUIDUnavailable
// and a zero Result (no implicit fallback to the current UID, ADR 0010).
// On timeout the child is SIGKILLed and the returned error is ErrTimedOut
// with TimedOut set.
//
// Resource limits are applied via prlimit(2) targeting the child PID
// immediately after Start. There is a tiny race window between fork and
// prlimit in which the child could exceed its limits — acceptable for an
// advisory cap on top of the UID-based trust boundary.
func (e *ProcessExecutor) Run(ctx context.Context, req Request) (Result, error) {
	euid := os.Geteuid()
	if euid != 0 && euid != e.AgentUID {
		return Result{}, ErrAgentUIDUnavailable
	}
	if req.NoNetwork && euid == 0 && !e.netNS {
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

	var cmd *exec.Cmd
	if req.Args != nil {
		cmd = exec.CommandContext(runCtx, req.Command, req.Args...)
	} else {
		cmd = exec.CommandContext(runCtx, "sh", "-c", req.Command)
	}
	attr := &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}
	if euid == 0 {
		// Setuid clears effective + permitted capabilities in the
		// child per capabilities(7); the bounding/inheritable sets are
		// not modified here (Go 1.26 has no SysProcAttr hook to clear
		// them before exec), but the child is no longer root so its
		// caps are inert. Single-account mode (euid != 0) needs no
		// Credential: the child inherits the process's own account.
		attr.Credential = &syscall.Credential{Uid: uint32(e.AgentUID), Gid: uint32(e.AgentUID)}
	}
	if req.NoNetwork && euid == 0 {
		// unshare runs in the child before the credential drop, while
		// CAP_SYS_ADMIN is still held: the child lands in a fresh,
		// empty network namespace (ADR 0010).
		attr.Unshareflags = syscall.CLONE_NEWNET
	}
	if req.NoNetwork && euid != 0 {
		// Unprivileged network isolation (ADR 0010): a user namespace
		// with the current UID/GID mapped to themselves is creatable
		// without privileges and owns the fresh, empty network
		// namespace cloned with it.
		attr.Cloneflags = syscall.CLONE_NEWUSER | syscall.CLONE_NEWNET
		attr.UidMappings = []syscall.SysProcIDMap{{ContainerID: euid, HostID: euid, Size: 1}}
		attr.GidMappings = []syscall.SysProcIDMap{{ContainerID: os.Getegid(), HostID: os.Getegid(), Size: 1}}
		attr.GidMappingsEnableSetgroups = false
	}
	cmd.SysProcAttr = attr
	if req.Stdin != nil {
		cmd.Stdin = bytes.NewReader(req.Stdin)
	}
	if req.Workdir != "" {
		cmd.Dir = req.Workdir
	}
	cmd.Env = tool.ChildEnv()
	if req.Env != nil {
		cmd.Env = append(cmd.Env, req.Env...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := cmd.Start
	if req.NoNetwork && euid != 0 {
		start = func() error { return startUsernsChild(cmd) }
	}
	if err := start(); err != nil {
		if req.NoNetwork && euid != 0 && errors.Is(err, syscall.EPERM) {
			// User namespaces are disabled on this host; fail closed
			// with the documented unavailability (ADR 0010).
			return Result{}, ErrAgentUIDUnavailable
		}
		return Result{}, fmt.Errorf("shell: start: %w", err)
	}

	// Resource caps bind untrusted code only (ADR 0010): a trusted
	// (operator-sanctioned) child is bounded by its per-call timeout
	// and the deployment's container/pod limits. Apply caps to the
	// now-running child via prlimit(2); individual failures are logged
	// at WARN — the run still produces a result, just without that cap.
	if !req.Trusted {
		e.applyRLimits(cmd.Process.Pid)
	}

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
// prlimit(2). A per-resource failure is logged at WARN and ignored: the
// child still runs, just without that one cap. Setting both Cur and Max
// to the same value produces a hard cap rather than a soft one with a
// higher hard ceiling.
//
// RLIMIT_NPROC is skipped in single-account mode: it counts every
// process of the real UID, so on a shared account the host's own
// processes would starve the child — the cap is only meaningful when the
// child owns a dedicated UID (the privileged drop).
func (e *ProcessExecutor) applyRLimits(pid int) {
	limits := []struct {
		name     string
		resource int
		rlim     *unix.Rlimit
	}{
		{"RLIMIT_CPU", unix.RLIMIT_CPU, cpuLimit(e.RLimits.CPU)},
		{"RLIMIT_AS", unix.RLIMIT_AS, &unix.Rlimit{
			Cur: uint64(e.RLimits.AddressSpace),
			Max: uint64(e.RLimits.AddressSpace),
		}},
		{"RLIMIT_FSIZE", unix.RLIMIT_FSIZE, &unix.Rlimit{
			Cur: uint64(e.RLimits.FileSize),
			Max: uint64(e.RLimits.FileSize),
		}},
		{"RLIMIT_NOFILE", unix.RLIMIT_NOFILE, &unix.Rlimit{
			Cur: e.RLimits.OpenFiles,
			Max: e.RLimits.OpenFiles,
		}},
	}
	if os.Geteuid() == 0 {
		limits = append(limits, struct {
			name     string
			resource int
			rlim     *unix.Rlimit
		}{"RLIMIT_NPROC", unix.RLIMIT_NPROC, &unix.Rlimit{
			Cur: e.RLimits.Processes,
			Max: e.RLimits.Processes,
		}})
	}
	for _, l := range limits {
		if err := unix.Prlimit(pid, l.resource, l.rlim, nil); err != nil {
			slog.Warn("shell: rlimit not applied",
				"resource", l.name, "pid", pid, "error", err)
		}
	}
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

// capSysAdmin is the bit position of CAP_SYS_ADMIN in the effective
// capability mask reported by /proc/self/status (capabilities(7)).
const capSysAdmin = 21

// netNSCapable reports whether this process can create a fresh network
// namespace: euid 0 with CAP_SYS_ADMIN in the effective set, the
// requirement of unshare(CLONE_NEWNET) (capabilities(7)). A read failure
// (e.g. non-Linux /proc shape) is treated as capable so the existing
// fork/exec error path still surfaces; the netns check is an opt-in
// refinement, not a load-bearing gate.
func netNSCapable() bool {
	if os.Geteuid() != 0 {
		return false
	}
	eff, err := readEffectiveCaps()
	if err != nil {
		return true
	}
	return eff&(1<<capSysAdmin) != 0
}

// readEffectiveCaps parses the CapEff line of /proc/self/status and
// returns the mask as a uint64.
func readEffectiveCaps() (uint64, error) {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, err
	}
	return parseCapEff(string(data))
}

// parseCapEff returns the CapEff mask from a /proc/self/status blob.
// Empty when the line is absent.
func parseCapEff(status string) (uint64, error) {
	for _, line := range strings.Split(status, "\n") {
		const prefix = "CapEff:"
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		hex := strings.TrimSpace(line[len(prefix):])
		// capabilities(7) reports the mask as 0x<hex>; tolerate plain
		// hex too.
		hex = strings.TrimPrefix(hex, "0x")
		hex = strings.TrimPrefix(hex, "0X")
		return strconv.ParseUint(hex, 16, 64)
	}
	return 0, errors.New("CapEff line not found")
}
