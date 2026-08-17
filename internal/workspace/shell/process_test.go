package shell_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/workspace/shell"
)

// fakeExecutor is the test double for the Executor interface. It records
// the last request and returns the canned result and error so callers
// can assert on the Request shape without a real fork+exec.
type fakeExecutor struct {
	req    shell.Request
	result shell.Result
	err    error
}

func (f *fakeExecutor) Run(_ context.Context, req shell.Request) (shell.Result, error) {
	f.req = req
	return f.result, f.err
}

// requireRoot skips the test when the current effective user cannot drop
// to the agent UID. The remaining branches still run; we just cannot
// exercise fork+exec without root.
func requireRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("setuid path requires root; run with `sudo -E go test ./internal/workspace/shell/...` to exercise fork+exec")
	}
}

func TestProcessExecutor_Success(t *testing.T) {
	requireRoot(t)
	exec := shell.NewProcessExecutor(shell.DefaultAgentUID, 5*time.Second)

	res, err := exec.Run(context.Background(), shell.Request{Command: "echo hello"})
	require.NoError(t, err)
	assert.Equal(t, "hello\n", res.Stdout)
	assert.Empty(t, res.Stderr)
	assert.Equal(t, 0, res.ExitCode)
	assert.False(t, res.TimedOut)
}

func TestProcessExecutor_NonZeroExit(t *testing.T) {
	requireRoot(t)
	exec := shell.NewProcessExecutor(shell.DefaultAgentUID, 5*time.Second)

	res, err := exec.Run(context.Background(), shell.Request{Command: "sh -c 'exit 7'"})
	require.NoError(t, err, "non-zero exits are not errors")
	assert.Equal(t, 7, res.ExitCode)
	assert.False(t, res.TimedOut)
}

func TestProcessExecutor_Timeout(t *testing.T) {
	requireRoot(t)
	exec := shell.NewProcessExecutor(shell.DefaultAgentUID, 5*time.Second)

	start := time.Now()
	res, err := exec.Run(context.Background(), shell.Request{
		Command: "sleep 5",
		Timeout: 100 * time.Millisecond,
	})
	elapsed := time.Since(start)

	require.ErrorIs(t, err, shell.ErrTimedOut)
	assert.True(t, res.TimedOut)
	assert.Less(t, elapsed, 3*time.Second, "timeout must kill the child promptly")
}

func TestProcessExecutor_AgentUIDUnavailable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("this case is only meaningful when the supervisor cannot drop privileges")
	}
	// The AgentUID must differ from both 0 and the process euid — any
	// fixed value could equal the test host's UID (the CI runner runs as
	// 1001, DefaultAgentUID itself) and turn the fail-closed case into
	// the legitimate single-account path.
	exec := shell.NewProcessExecutor(os.Geteuid()+1, 5*time.Second)

	_, err := exec.Run(context.Background(), shell.Request{Command: "echo hi"})

	require.ErrorIs(t, err, shell.ErrAgentUIDUnavailable,
		"exec-uid different from the euid without privilege fails closed — no implicit fallback to the current UID")
}

// requireUnprivileged skips the single-account tests when running as
// root: the mode exists for hosts that cannot drop privileges.
func requireUnprivileged(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("single-account mode is only meaningful without root")
	}
}

// TestProcessExecutor_UntrustedChildCarriesDefaultCaps pins the ADR 0010
// trust-class rule on the untrusted path: an agent-supplied command
// runs under the executor's resource caps, and carries them from its
// first instruction — the shim applies them before exec, so a plain
// `ulimit -v` reads the cap with nothing to race (issue #401).
func TestProcessExecutor_UntrustedChildCarriesDefaultCaps(t *testing.T) {
	requireUnprivileged(t)
	exec := shell.NewProcessExecutor(os.Geteuid(), 5*time.Second)

	res, err := exec.Run(context.Background(), shell.Request{Command: "ulimit -v"})
	require.NoError(t, err)
	assert.Equal(t, "524288\n", res.Stdout,
		"the untrusted child carries the 512MiB address-space cap")
}

// TestProcessExecutor_UntrustedChildCarriesEveryCap reads the caps from
// the kernel's own view of the child. Single-account mode carries four:
// RLIMIT_NPROC counts the shared account's processes, so it applies
// only to the privileged drop (ADR 0010).
func TestProcessExecutor_UntrustedChildCarriesEveryCap(t *testing.T) {
	requireUnprivileged(t)
	exec := shell.NewProcessExecutor(os.Geteuid(), 5*time.Second)

	res, err := exec.Run(context.Background(), shell.Request{Command: "cat /proc/self/limits"})
	require.NoError(t, err)
	for _, want := range []string{
		`Max cpu time +60 +60 `,
		`Max address space +536870912 +536870912 `,
		`Max file size +104857600 +104857600 `,
		`Max open files +1024 +1024 `,
	} {
		assert.Regexp(t, want, res.Stdout)
	}
}

// TestProcessExecutor_UncappableChildFailsClosed pins the posture ADR
// 0008 sets for a boundary mechanism the platform cannot deliver: the
// call fails and no command runs, rather than a warning behind an
// uncapped child. An open-file cap above the hard limit is the
// unprivileged way to make the child's setrlimit fail.
func TestProcessExecutor_UncappableChildFailsClosed(t *testing.T) {
	requireUnprivileged(t)
	exec := shell.NewProcessExecutor(os.Geteuid(), 5*time.Second)
	var hard syscall.Rlimit
	require.NoError(t, syscall.Getrlimit(syscall.RLIMIT_NOFILE, &hard))
	exec.RLimits.OpenFiles = hard.Max + 1

	marker := filepath.Join(t.TempDir(), "ran")
	_, err := exec.Run(context.Background(), shell.Request{Command: "touch " + marker})

	require.ErrorIs(t, err, shell.ErrRlimitsUnavailable)
	assert.NoFileExists(t, marker, "the command must not run when a cap cannot be applied")
}

// TestProcessExecutor_TrustedChildRunsWithoutCaps pins the other half:
// an operator-sanctioned (script-tool) child runs without the
// adversarial caps — its bound is the per-call timeout.
func TestProcessExecutor_TrustedChildRunsWithoutCaps(t *testing.T) {
	requireUnprivileged(t)
	exec := shell.NewProcessExecutor(os.Geteuid(), 5*time.Second)

	res, err := exec.Run(context.Background(), shell.Request{Command: "ulimit -v", Trusted: true})
	require.NoError(t, err)
	assert.Equal(t, "unlimited\n", res.Stdout,
		"the trusted child carries no address-space cap")
}

// buildGoHelper compiles a trivial Go binary for the trust-class
// acceptance case: any Go binary stands in for the QA scenario's `gh`.
func buildGoHelper(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	main := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(main,
		[]byte("package main\n\nfunc main() { println(\"helper-ok\") }\n"), 0o644))
	out := filepath.Join(dir, "helper")
	cmd := exec.Command("go", "build", "-o", out, main)
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		t.Skipf("go toolchain unavailable for the helper build: %v", err)
	}
	return out
}

// TestProcessExecutor_TrustedGoBinaryRuns is the issue #225 acceptance
// case: a Go binary runs as a trusted (script-tool-class) child. The
// untrusted-path death under the 512MiB cap is not asserted — it depends
// on the runner's Go runtime reservation behavior (issue #244); the caps
// themselves are pinned by the ulimit tests above.
func TestProcessExecutor_TrustedGoBinaryRuns(t *testing.T) {
	requireUnprivileged(t)
	helper := buildGoHelper(t)
	exec := shell.NewProcessExecutor(os.Geteuid(), 5*time.Second)

	res, err := exec.Run(context.Background(), shell.Request{Command: helper, Trusted: true})
	require.NoError(t, err)
	assert.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "helper-ok\n", res.Stderr,
		"the Go helper runs to completion as a trusted child")
}

// TestProcessExecutor_SingleAccountRunsAsOwnUID pins the single-account
// mode (ADR 0008): with AgentUID equal to the process euid, an
// unprivileged executor runs children as its own account — no privilege
// required, no agent_uid unavailable.
func TestProcessExecutor_SingleAccountRunsAsOwnUID(t *testing.T) {
	requireUnprivileged(t)
	exec := shell.NewProcessExecutor(os.Geteuid(), 5*time.Second)

	res, err := exec.Run(context.Background(), shell.Request{Command: "echo -n $(id -u):$(id -g)"})
	require.NoError(t, err)
	// The child's own output is in the message because the two ways this
	// assertion fails look identical without it: a child that ran as the
	// wrong account, and a child that never ran at all (issue #522).
	assert.Equal(t,
		fmt.Sprintf("%d:%d", os.Geteuid(), os.Getegid()), res.Stdout,
		"the child runs as the agent's own account in single-account mode (exit=%d, stderr=%q)",
		res.ExitCode, res.Stderr)
	assert.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
}

func TestProcessExecutor_DefaultTimeoutRejectsZero(t *testing.T) {
	requireRoot(t)
	exec := shell.NewProcessExecutor(shell.DefaultAgentUID, 0)

	_, err := exec.Run(context.Background(), shell.Request{Command: "echo hi"})

	require.Error(t, err, "a missing default timeout must fail closed, not run forever")
}

func TestProcessExecutor_RunsInWorkspace(t *testing.T) {
	requireRoot(t)
	root := t.TempDir()
	require.NoError(t, os.WriteFile(root+"/marker.txt", []byte("in-workspace"), 0o644))
	exec := shell.NewProcessExecutor(shell.DefaultAgentUID, 5*time.Second)

	res, err := exec.Run(context.Background(), shell.Request{
		Command: "cat marker.txt",
		Workdir: root,
	})

	require.NoError(t, err)
	assert.Equal(t, "in-workspace", res.Stdout)
}

func TestProcessExecutor_ScrubsPlatformEnv(t *testing.T) {
	requireRoot(t)
	t.Setenv("GAGENT_TEST_PLATFORM_SECRET", "hunter2")
	exec := shell.NewProcessExecutor(shell.DefaultAgentUID, 5*time.Second)

	res, err := exec.Run(context.Background(), shell.Request{Command: "env"})

	require.NoError(t, err)
	assert.NotContains(t, res.Stdout, "GAGENT_TEST_PLATFORM_SECRET",
		"platform variables must not reach child processes (ADR 0003)")
	assert.Contains(t, res.Stdout, "PATH=", "the minimal basics still reach the child")
}

func TestProcessExecutor_RequestEnvAppliesOnTop(t *testing.T) {
	requireRoot(t)
	exec := shell.NewProcessExecutor(shell.DefaultAgentUID, 5*time.Second)

	res, err := exec.Run(context.Background(), shell.Request{
		Command: "printf %s \"$GAGENT_TEST_REQUEST_VAR\"",
		Env:     []string{"GAGENT_TEST_REQUEST_VAR=from-request"},
	})

	require.NoError(t, err)
	assert.Equal(t, "from-request", res.Stdout)
}

// TestFakeExecutor_NoNetworkPassesThrough pins the interface shape, not
// the namespace: Request.NoNetwork must cross the Executor boundary
// verbatim, so a caller's request for isolation is observable to any
// implementation. No process runs here — the double records the request
// and returns a canned result. What a real namespace contains is pinned
// by the root and userns tests in netns_linux_test.go.
func TestFakeExecutor_NoNetworkPassesThrough(t *testing.T) {
	exec := &fakeExecutor{}
	_, err := exec.Run(context.Background(), shell.Request{Command: "echo", NoNetwork: true})
	require.NoError(t, err)
	assert.True(t, exec.req.NoNetwork,
		"the executor carries the caller's NoNetwork flag")
}
