package shell_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/workspace/shell"
)

// testExecUID is the account these tests ask the executor to drop to.
// The platform declares no default of its own — the exec UID reaches
// the workspace from config (internal/config), so the value a test
// wants is the test's to name.
const testExecUID = 1001

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

// privilegedRun reports whether this run promised to reach the privileged
// boundary. `make test-privileged` sets the variable; a guard that would
// otherwise skip fails instead, because a privileged case that quietly
// skips inside a run commissioned to exercise it is the defect #604
// reports, one layer up.
func privilegedRun() bool {
	return os.Getenv("SHERLOCK_PRIVILEGED_TESTS") != ""
}

// requireRoot skips the test when the current effective user cannot drop
// to the agent UID. The remaining branches still run; we just cannot
// exercise fork+exec without root. In a run that declared itself
// privileged there is nothing to fall back to, so the absent privilege is
// the result.
func requireRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		return
	}
	if privilegedRun() {
		t.Fatalf("SHERLOCK_PRIVILEGED_TESTS is set but this run has euid %d: the privileged family cannot be exercised and must not report as if it were",
			os.Geteuid())
	}
	t.Skip("setuid path requires root; run `make test-privileged` to exercise fork+exec")
}

// execAccountDir returns a directory the child's account can traverse.
// `t.TempDir` roots its per-test tree at mode 0700 owned by the test's own
// account, so a child dropped to another UID cannot enter it and fails on
// the permission rather than on what the case is about. A privileged
// workspace has the same problem and solves it by handing the workspace
// directory to the exec account (ADR 0008); these tests hold no workspace,
// so they open the path instead.
func execAccountDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.Chmod(filepath.Dir(dir), 0o755), "open the per-test root")
	require.NoError(t, os.Chmod(dir, 0o755), "open the directory itself")
	return dir
}

func TestProcessExecutor_Success(t *testing.T) {
	requireRoot(t)
	exec := shell.NewProcessExecutor(testExecUID, 5*time.Second)

	res, err := exec.Run(context.Background(), shell.Request{Command: "echo hello"})
	require.NoError(t, err)
	assert.Equal(t, "hello\n", res.Stdout)
	assert.Empty(t, res.Stderr)
	assert.Equal(t, 0, res.ExitCode)
	assert.False(t, res.TimedOut)
}

func TestProcessExecutor_NonZeroExit(t *testing.T) {
	requireRoot(t)
	exec := shell.NewProcessExecutor(testExecUID, 5*time.Second)

	res, err := exec.Run(context.Background(), shell.Request{Command: "sh -c 'exit 7'"})
	require.NoError(t, err, "non-zero exits are not errors")
	assert.Equal(t, 7, res.ExitCode)
	assert.False(t, res.TimedOut)
}

func TestProcessExecutor_Timeout(t *testing.T) {
	requireRoot(t)
	exec := shell.NewProcessExecutor(testExecUID, 5*time.Second)

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
	// 1001, testExecUID itself) and turn the fail-closed case into
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
	assert.Equal(t, "2097152\n", res.Stdout,
		"the untrusted child carries the 2GiB address-space cap")
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
		`Max address space +2147483648 +2147483648 `,
		`Max file size +104857600 +104857600 `,
		`Max open files +1024 +1024 `,
	} {
		assert.Regexp(t, want, res.Stdout)
	}
}

// TestProcessExecutor_StdinReachesTheChild pins that Request.Stdin is
// what the child reads on its standard input. Nothing asserted it
// before (survey process:171): no test in the repository set the field,
// so inverting the plumbing that hands it over left the whole suite
// green.
//
// `cat` is the assertion's other half. With stdin attached it writes
// back what it was given; with none it reads EOF and writes nothing, so
// a request whose bytes never reach the child produces empty stdout
// rather than a different error.
func TestProcessExecutor_StdinReachesTheChild(t *testing.T) {
	requireUnprivileged(t)
	exec := shell.NewProcessExecutor(os.Geteuid(), 5*time.Second)

	res, err := exec.Run(context.Background(), shell.Request{
		Command: "cat",
		Stdin:   []byte("the request's own bytes"),
	})
	require.NoError(t, err)
	assert.Equal(t, "the request's own bytes", res.Stdout)
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
// case: a Go binary runs as a trusted (script-tool-class) child.
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

// TestProcessExecutor_PrivilegedDropRunsAsExecUID pins the ADR 0008 drop
// itself: a privileged executor's child runs as the configured exec
// account and not as the account the executor runs as. Nothing else in
// this package can pin it. The single-account case below asserts the
// child's uid too, but there the two accounts are one number, so its
// assertion holds whether or not the drop happens — which is how deleting
// the drop left the whole suite green (#604).
func TestProcessExecutor_PrivilegedDropRunsAsExecUID(t *testing.T) {
	requireRoot(t)
	exec := shell.NewProcessExecutor(testExecUID, 5*time.Second)

	res, err := exec.Run(context.Background(), shell.Request{Command: "id -u; id -g"})

	require.NoError(t, err)
	// The child's own output is in the message for the reason the
	// single-account case gives: a child that ran as the wrong account and
	// a child that never ran look identical without it (issue #522).
	assert.Equal(t, fmt.Sprintf("%d\n%d\n", testExecUID, testExecUID), res.Stdout,
		"the child drops to the exec account and does not keep the executor's (exit=%d, stderr=%q)",
		res.ExitCode, res.Stderr)
	assert.NotEqual(t, os.Geteuid(), testExecUID,
		"this case is only meaningful when the two accounts differ")
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
	exec := shell.NewProcessExecutor(testExecUID, 0)

	_, err := exec.Run(context.Background(), shell.Request{Command: "echo hi"})

	require.Error(t, err, "a missing default timeout must fail closed, not run forever")
}

func TestProcessExecutor_RunsInWorkspace(t *testing.T) {
	requireRoot(t)
	root := execAccountDir(t)
	require.NoError(t, os.WriteFile(root+"/marker.txt", []byte("in-workspace"), 0o644))
	exec := shell.NewProcessExecutor(testExecUID, 5*time.Second)

	res, err := exec.Run(context.Background(), shell.Request{
		Command: "cat marker.txt",
		Workdir: root,
	})

	require.NoError(t, err)
	assert.Equal(t, "in-workspace", res.Stdout)
}

func TestProcessExecutor_ScrubsPlatformEnv(t *testing.T) {
	requireRoot(t)
	t.Setenv("SHERLOCK_TEST_PLATFORM_SECRET", "hunter2")
	exec := shell.NewProcessExecutor(testExecUID, 5*time.Second)

	res, err := exec.Run(context.Background(), shell.Request{Command: "env"})

	require.NoError(t, err)
	assert.NotContains(t, res.Stdout, "SHERLOCK_TEST_PLATFORM_SECRET",
		"platform variables must not reach child processes (ADR 0003)")
	assert.Contains(t, res.Stdout, "PATH=", "the minimal basics still reach the child")
}

func TestProcessExecutor_RequestEnvAppliesOnTop(t *testing.T) {
	requireRoot(t)
	exec := shell.NewProcessExecutor(testExecUID, 5*time.Second)

	res, err := exec.Run(context.Background(), shell.Request{
		Command: "printf %s \"$SHERLOCK_TEST_REQUEST_VAR\"",
		Env:     []string{"SHERLOCK_TEST_REQUEST_VAR=from-request"},
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

func TestProcessExecutor_LocalGoTest(t *testing.T) {
	uid := os.Geteuid()
	if uid == 0 {
		uid = testExecUID
	}
	root := execAccountDir(t)
	t.Setenv("SHERLOCK_TEST_PLATFORM_SECRET", "must-not-reach-test")
	goRoot, err := exec.Command("go", "env", "GOROOT").Output()
	require.NoError(t, err, "locate the test runner toolchain")
	goBinary := filepath.Join(strings.TrimSpace(string(goRoot)), "bin", "go")
	source := fmt.Sprintf(`package sample
import (
    "os"
    "testing"
)
func TestGreeting(t *testing.T) {
    if os.Getuid() != %d { t.Fatalf("unexpected uid: %%d", os.Getuid()) }
    if os.Getenv("SHERLOCK_TEST_PLATFORM_SECRET") != "" { t.Fatal("platform environment leaked") }
    if greeting() != "hello" { t.Fatal("wrong greeting") }
    t.Log("workspace-test-ran")
}
`, uid)
	require.NoError(t, os.WriteFile(filepath.Join(root, "greeting_test.go"), []byte(source), 0o644))
	program := filepath.Join(root, "greeting.go")
	require.NoError(t, os.WriteFile(program, []byte("package sample\nfunc greeting() string { return \"hello\" }\n"), 0o644))
	executor := shell.NewProcessExecutor(uid, 2*time.Minute)
	if os.Geteuid() == 0 {
		require.NoError(t, os.Chown(root, uid, uid))
		t.Cleanup(func() {
			res, err := executor.Run(context.Background(), shell.Request{
				Command: "rm -rf cache control-cache", Workdir: root, Trusted: true,
			})
			require.NoError(t, err)
			require.Equal(t, 0, res.ExitCode, "cache cleanup: %s", res.Stderr)
			require.NoError(t, os.Chown(root, os.Geteuid(), os.Getegid()))
		})
	}
	req := shell.Request{
		Command: goBinary,
		Args:    []string{"test", "-p=1", "-count=1", "-v"},
		Workdir: root,
		Env:     []string{"GO111MODULE=off", "GOTOOLCHAIN=local", "GOENV=off", "CGO_ENABLED=0", "GOMAXPROCS=2", "GOCACHE=" + filepath.Join(root, "cache")},
	}
	t.Run("control without resource caps", func(t *testing.T) {
		control := req
		control.Trusted = true
		control.Env = append(append([]string{}, req.Env...), "GOCACHE="+filepath.Join(root, "control-cache"))
		res, err := executor.Run(context.Background(), control)
		require.NoError(t, err)
		require.Equal(t, 0, res.ExitCode, "stdout=%s stderr=%s", res.Stdout, res.Stderr)
		require.Contains(t, res.Stdout, "--- PASS: TestGreeting")
	})
	for _, noNetwork := range []bool{false, true} {
		t.Run(fmt.Sprintf("bounded no-network=%t", noNetwork), func(t *testing.T) {
			req.NoNetwork = noNetwork
			res := runProbe(t, executor, req)
			require.Equal(t, 0, res.ExitCode, "stdout=%s stderr=%s", res.Stdout, res.Stderr)
			require.Contains(t, res.Stdout, "workspace-test-ran")
			require.Contains(t, res.Stdout, "--- PASS: TestGreeting")
		})
	}
	t.Run("failing test reports failure", func(t *testing.T) {
		require.NoError(t, os.WriteFile(program, []byte("package sample\nfunc greeting() string { return \"wrong\" }\n"), 0o644))
		req.NoNetwork = false
		res, err := executor.Run(context.Background(), req)
		require.NoError(t, err)
		require.Equal(t, 1, res.ExitCode, "stdout=%s stderr=%s", res.Stdout, res.Stderr)
		require.Contains(t, res.Stdout, "--- FAIL: TestGreeting")
		require.Contains(t, res.Stdout, "wrong greeting")
	})
}
