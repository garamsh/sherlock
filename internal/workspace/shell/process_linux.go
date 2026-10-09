//go:build linux

package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
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
		AddressSpace: 2 * 1024 * 1024 * 1024, // 2 GiB virtual memory, including Go runtime reservations.
		FileSize:     100 * 1024 * 1024,      // 100 MiB per output file.
		OpenFiles:    1024,
		Processes:    64,
	}
}

// ProcessExecutor runs commands via fork+exec+setuid, dropping privileges
// to AgentUID and applying resource limits per ADR 0008 and ADR 0010. It
// is the production Executor implementation.
//
// When AgentUID equals the process's euid the deployment declares a
// single-account run (ADR 0008): no UID switch is needed and no
// privilege is required — children run as the agent's own account.
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
	// canSignal is true when this process holds CAP_KILL, which is what
	// it takes to signal a child it dropped to another account. Without
	// it the per-call deadline cannot be enforced, so Run fails closed
	// rather than starting a command nothing can stop (#612).
	// NewProcessExecutor detects it once at construction; tests set it
	// directly.
	canSignal bool
	// shimPath is this binary, which every untrusted child execs first
	// to apply its own caps (ADR 0008). Resolved once at construction;
	// shimErr carries a resolution failure to Run, which refuses the
	// call rather than running the child uncapped.
	shimPath string
	shimErr  error
}

// NewProcessExecutor returns a ProcessExecutor configured with the given
// agent UID and default per-call timeout. It does not validate
// privilege — Run reports ErrAgentUIDUnavailable when the caller cannot
// drop to AgentUID, when it cannot create the fresh network namespace
// required for NoNetwork, and when it could not signal the account it
// drops to. The two capability probes run here, once, and each warns when
// what it found will make calls fail; in the unprivileged shape the
// NoNetwork probe runs here too, once, and warns the same way (ADR 0010).
func NewProcessExecutor(agentUID int, defaultTimeout time.Duration) *ProcessExecutor {
	self, err := os.Executable()
	exec := &ProcessExecutor{
		AgentUID:       agentUID,
		DefaultTimeout: defaultTimeout,
		RLimits:        DefaultRlimits(),
		netNS:          netNSCapable(),
		canSignal:      signalCapable(),
		shimPath:       self,
		shimErr:        err,
	}
	if !exec.netNS && os.Geteuid() == 0 {
		slog.Warn("shell: CAP_SYS_ADMIN missing; netns-isolated children will report agent_uid unavailable",
			"reason", "unshare(CLONE_NEWNET) requires CAP_SYS_ADMIN")
	}
	if !exec.canSignal && exec.dropsAccount() {
		slog.Warn("shell: CAP_KILL missing; every Exec will report agent_uid unavailable",
			"reason", "signalling a child dropped to another account requires CAP_KILL, so its timeout cannot be enforced")
	}
	if euid := os.Geteuid(); euid != 0 && agentUID == euid {
		if err := probeNoNetwork(exec); err != nil {
			slog.Warn("shell: NoNetwork probe failed; every NoNetwork Exec, so every shell_exec, will fail with this error (docs/architecture/deployment.md §Capabilities)",
				"error", err,
				"reason", "an unprivileged NoNetwork child is cloned CLONE_NEWUSER|CLONE_NEWNET, which user.max_user_namespaces, the seccomp profile and any LSM must all admit")
		}
	}
	return exec
}

// probeNoNetwork makes the clone a NoNetwork Exec makes by running one
// through Run, so what it meets is what every such call would meet. It is
// a variable so a test can stand in for a host that refuses the clone.
var probeNoNetwork = func(e *ProcessExecutor) error {
	_, err := e.Run(context.Background(), Request{Command: "exit 0", NoNetwork: true, Trusted: true, Timeout: 5 * time.Second})
	return err
}

// dropsAccount reports whether this executor's children run under an
// account other than its own — the privileged drop, where a signal from
// the executor crosses a uid boundary. Single-account mode does not: the
// child owns the executor's own uid, which is permission enough.
func (e *ProcessExecutor) dropsAccount() bool {
	euid := os.Geteuid()
	return euid == 0 && euid != e.AgentUID
}

// Run executes one command and returns its captured output. If the
// caller cannot run the child as AgentUID — AgentUID differs from the
// euid and the process is not root — Run returns ErrAgentUIDUnavailable
// and a zero Result (no implicit fallback to the current UID, ADR 0010).
// On timeout the child's process group is SIGKILLed and the returned error
// is ErrTimedOut with TimedOut set; a privileged executor that holds no
// CAP_KILL cannot perform that kill, so it refuses the call instead of
// running a command whose deadline would pass unenforced (#612).
//
// An untrusted child execs this binary's shim first, which applies the
// resource caps to itself before exec'ing the command, so the caps are
// in force from the command's first instruction. A cap that cannot be
// applied returns ErrRlimitsUnavailable and no command runs (ADR 0008).
func (e *ProcessExecutor) Run(ctx context.Context, req Request) (Result, error) {
	euid := os.Geteuid()
	if euid != 0 && euid != e.AgentUID {
		return Result{}, ErrAgentUIDUnavailable
	}
	if req.NoNetwork && euid == 0 && !e.netNS {
		return Result{}, ErrAgentUIDUnavailable
	}
	if e.dropsAccount() && !e.canSignal {
		return Result{}, fmt.Errorf("%w: CAP_KILL is absent, so a child dropped to uid %d cannot be signalled and its deadline cannot be enforced",
			ErrAgentUIDUnavailable, e.AgentUID)
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

	name, argv := "sh", []string{"sh", "-c", req.Command}
	if req.Args != nil {
		name, argv = req.Command, append([]string{req.Command}, req.Args...)
	}
	var cmd *exec.Cmd
	if req.Trusted {
		// Resource caps bind untrusted code only (ADR 0010): a
		// trusted (operator-sanctioned) child is bounded by its
		// per-call timeout and the deployment's container/pod limits,
		// so it execs its command directly.
		cmd = exec.CommandContext(runCtx, name, argv[1:]...)
	} else {
		shimmed, err := e.shimCommandLine(name, argv, euid)
		if err != nil {
			return Result{}, err
		}
		cmd = exec.CommandContext(runCtx, shimmed[0], shimmed[1:]...)
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
	// On expiry the whole process group goes, not the child alone (ADR
	// 0008). The child leads its own group — that is what Setpgid above is
	// for — and an untrusted child is the shim, which execs a shell, which
	// execs the command: every one of them inherits the pipe Wait drains,
	// so killing the first alone leaves Run blocked until the last exits.
	// A timeout the caller waits out is not a timeout.
	cmd.Cancel = func() error {
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return err
		}
		return os.ErrProcessDone
	}
	if req.Stdin != nil {
		cmd.Stdin = bytes.NewReader(req.Stdin)
	}
	if req.Workdir != "" {
		cmd.Dir = req.Workdir
	}
	cmd.Env = childEnv()
	if req.Env != nil {
		cmd.Env = append(cmd.Env, req.Env...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// The shim reports a cap it could not apply on this descriptor and
	// closes it at exec, so silence is the child's "caps in force".
	var statusR, statusW *os.File
	if !req.Trusted {
		var err error
		statusR, statusW, err = os.Pipe()
		if err != nil {
			return Result{}, fmt.Errorf("%w: status pipe: %v", ErrRlimitsUnavailable, err)
		}
		defer statusR.Close()
		defer statusW.Close()
		cmd.ExtraFiles = []*os.File{statusW}
	}

	start := cmd.Start
	if req.NoNetwork && euid != 0 {
		start = func() error { return startUserns(cmd) }
	}
	if err := start(); err != nil {
		if req.NoNetwork && euid != 0 && usernsRefused(err) {
			// The host will not give the child its namespaces; fail
			// closed with the documented unavailability (ADR 0010).
			return Result{}, fmt.Errorf("%w: %v", ErrAgentUIDUnavailable, err)
		}
		return Result{}, fmt.Errorf("shell: start: %w", err)
	}

	if statusR != nil {
		statusW.Close()
		if err := shimStatus(statusR); err != nil {
			_ = cmd.Wait()
			return Result{}, err
		}
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

// startUserns starts a NoNetwork child in the unprivileged shape. It is a
// variable so a test can stand in for a host that refuses the clone.
var startUserns = startUsernsChild

// usernsRefused reports whether a failed CLONE_NEWUSER|CLONE_NEWNET start
// means the host will not give the child its namespaces (clone(2)): EPERM
// from the sysctl, a seccomp profile or an LSM; ENOSPC, or EUSERS on
// older kernels, over user.max_user_namespaces; EINVAL from a kernel
// built without user or network namespaces.
func usernsRefused(err error) bool {
	for _, errno := range []syscall.Errno{syscall.EPERM, syscall.ENOSPC, syscall.EUSERS, syscall.EINVAL} {
		if errors.Is(err, errno) {
			return true
		}
	}
	return false
}

// shimCommandLine renders the command line an untrusted child runs:
// this binary as the resource-limit shim, the caps it must apply, and
// the program it execs afterwards. The program is resolved here,
// against the platform's own PATH, so a request's Env cannot redirect
// which binary the caps end up protecting.
func (e *ProcessExecutor) shimCommandLine(name string, argv []string, euid int) ([]string, error) {
	if e.shimErr != nil {
		return nil, fmt.Errorf("%w: locating this binary: %v", ErrRlimitsUnavailable, e.shimErr)
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("shell: start: %w", err)
	}
	return shimArgv(e.shimPath, e.limitSpecs(euid), path, argv), nil
}

// limitSpecs is the cap set an untrusted child carries. RLIMIT_NPROC
// joins only in the privileged drop: it counts every process of the
// real UID, so on a shared account the host's own processes would
// starve the child — the cap is only meaningful when the child owns a
// dedicated UID.
func (e *ProcessExecutor) limitSpecs(euid int) []limitSpec {
	specs := []limitSpec{
		{name: "cpu", value: cpuSeconds(e.RLimits.CPU)},
		{name: "as", value: uint64(e.RLimits.AddressSpace)},
		{name: "fsize", value: uint64(e.RLimits.FileSize)},
		{name: "nofile", value: e.RLimits.OpenFiles},
	}
	if euid == 0 {
		specs = append(specs, limitSpec{name: "nproc", value: e.RLimits.Processes})
	}
	return specs
}

// shimStatus reads what the shim reported. The shim closes the
// descriptor at exec, so an empty read means the caps are in force;
// anything else is the reason they are not, and the command never ran.
func shimStatus(status *os.File) error {
	reported, err := io.ReadAll(status)
	if err != nil {
		return fmt.Errorf("%w: reading the shim's status: %v", ErrRlimitsUnavailable, err)
	}
	if len(reported) > 0 {
		return fmt.Errorf("%w: %s", ErrRlimitsUnavailable, strings.TrimSpace(string(reported)))
	}
	return nil
}

// cpuSeconds converts a duration into the seconds unit the kernel
// expects for RLIMIT_CPU. A zero or negative duration produces a
// 1-second cap rather than "unlimited" — the child should never run
// forever.
func cpuSeconds(d time.Duration) uint64 {
	seconds := uint64(d / time.Second)
	if d%time.Second != 0 {
		seconds++
	}
	if seconds == 0 {
		seconds = 1
	}
	return seconds
}

// capSysAdmin and capKill are the bit positions of CAP_SYS_ADMIN and
// CAP_KILL in the effective capability mask reported by
// /proc/self/status (capabilities(7)).
const (
	capKill     = 5
	capSysAdmin = 21
)

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

// signalCapable reports whether this process holds CAP_KILL. kill(2)
// permits a signal only when the sender's real or effective uid matches
// the target's real or saved-set uid, or the sender holds CAP_KILL; a
// child dropped to the exec account owns neither of the executor's uids,
// so the capability is the only route, and being root is no substitute
// once it has been dropped from the bounding set (capabilities(7)). A
// read failure is treated as capable, as netNSCapable treats one: the
// check refines a diagnosis rather than standing as the boundary.
func signalCapable() bool {
	eff, err := readEffectiveCaps()
	if err != nil {
		return true
	}
	return eff&(1<<capKill) != 0
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
