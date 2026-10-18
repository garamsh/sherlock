package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/workspace/shell"
)

// TestMain routes the resource-limit verb as main does: the workspace
// these tests start runs a real executor, which execs whichever binary
// it runs as — this test binary — to cap each child (ADR 0008).
func TestMain(m *testing.M) {
	shell.ShimIfRequested()
	os.Exit(m.Run())
}

// operationRun is what one operation subcommand left behind.
type operationRun struct {
	stdout string
	stderr string
	status int
}

// failure decodes the run's stderr as the one JSON failure object.
func (r operationRun) failure(t *testing.T) operationError {
	t.Helper()
	var got operationError
	dec := json.NewDecoder(strings.NewReader(r.stderr))
	require.NoError(t, dec.Decode(&got), "stderr must be one JSON object: %q", r.stderr)
	assert.False(t, dec.More(), "stderr must hold exactly one JSON object: %q", r.stderr)
	return got
}

// runOp runs `sherlock workspace <args>` through the binary's
// command tree with stdin, and returns its output and the exit status
// main would give it.
func runOp(t *testing.T, stdin string, args ...string) operationRun {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand()
	root.SetIn(strings.NewReader(stdin))
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"workspace"}, args...))
	err := root.Execute()
	return operationRun{stdout: stdout.String(), stderr: stderr.String(), status: exitStatus(err)}
}

// unusedAddr reserves a loopback address and lets it go, so nothing
// listens there.
func unusedAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())
	return addr
}

// startWorkspace serves a workspace with `sherlock workspace` on a free
// loopback address, in single-account mode so no privilege is needed,
// and returns the address and the workspace directory.
func startWorkspace(t *testing.T) (addr, dir string) {
	t.Helper()
	addr = unusedAddr(t)
	dir = filepath.Join(t.TempDir(), "workspace")
	configPath := filepath.Join(t.TempDir(), "workspace.yaml")
	require.NoError(t, os.WriteFile(configPath, nil, 0o600))

	root := NewRootCommand()
	root.SetArgs([]string{"workspace", "--config", configPath, "--addr", addr,
		"--workspace", dir, "--exec-uid", strconv.Itoa(os.Geteuid())})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			assert.NoError(t, err, "the workspace must stop cleanly")
		case <-time.After(15 * time.Second):
			t.Error("the workspace did not stop on cancellation")
		}
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			require.NoError(t, conn.Close())
			return addr, dir
		}
		select {
		case err := <-done:
			t.Fatalf("the workspace exited before listening: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("the workspace at %s did not listen within 5s: %v", addr, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// requireUnreachable asserts the unreachable-workspace contract for one
// operation: exit 3, the JSON failure naming the class, nothing on stdout.
func requireUnreachable(t *testing.T, args ...string) {
	t.Helper()
	run := runOp(t, "", append(args, "--workspace-addr", unusedAddr(t))...)
	assert.Equal(t, 3, run.status)
	assert.Empty(t, run.stdout)
	got := run.failure(t)
	assert.Equal(t, classUnreachable, got.Class)
	assert.Equal(t, "Unavailable", got.Code)
}

// requireRefused asserts the workspace-reported failure contract: exit
// 2, the JSON failure carrying the workspace's code, nothing on stdout.
func requireRefused(t *testing.T, run operationRun, code string) operationError {
	t.Helper()
	assert.Equal(t, 2, run.status)
	assert.Empty(t, run.stdout)
	got := run.failure(t)
	assert.Equal(t, classWorkspace, got.Class)
	assert.Equal(t, code, got.Code)
	return got
}

// TestWorkspaceRead_PrintsBytesAndSeparatesRefusalFromUnreachable reads
// a file that is not UTF-8 back unchanged, refuses a path outside the
// workspace with exit 2, and reports a workspace that is not listening
// with exit 3 (ADR 0027). The successful read is the refusal's control.
func TestWorkspaceRead_PrintsBytesAndSeparatesRefusalFromUnreachable(t *testing.T) {
	addr, dir := startWorkspace(t)
	content := "hello\x00\xff\xfe"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "note.bin"), []byte(content), 0o644))

	run := runOp(t, "", "read", "note.bin", "--workspace-addr", addr)
	require.Equal(t, 0, run.status, "stderr: %s", run.stderr)
	assert.Equal(t, content, run.stdout)
	assert.Empty(t, run.stderr)

	got := requireRefused(t, runOp(t, "", "read", "../note.bin", "--workspace-addr", addr), "InvalidArgument")
	assert.Contains(t, got.Message, "escapes workspace")

	requireUnreachable(t, "read", "note.bin")
}

// TestWorkspaceWrite_WritesStdinAndSeparatesRefusalFromUnreachable
// writes standard input into the workspace, refuses a path outside it
// without writing there, and reports an unreachable workspace with
// exit 3 (ADR 0027).
func TestWorkspaceWrite_WritesStdinAndSeparatesRefusalFromUnreachable(t *testing.T) {
	addr, dir := startWorkspace(t)

	run := runOp(t, "line one\n", "write", "sub/out.txt", "--workspace-addr", addr)
	require.Equal(t, 0, run.status, "stderr: %s", run.stderr)
	assert.JSONEq(t, `{"bytes_written":9}`, run.stdout)
	written, err := os.ReadFile(filepath.Join(dir, "sub", "out.txt"))
	require.NoError(t, err)
	assert.Equal(t, "line one\n", string(written))

	requireRefused(t, runOp(t, "x", "write", "../escape.txt", "--workspace-addr", addr), "InvalidArgument")
	assert.NoFileExists(t, filepath.Join(filepath.Dir(dir), "escape.txt"))

	requireUnreachable(t, "write", "out.txt")
}

// TestWorkspaceList_PrintsEntriesAndSeparatesRefusalFromUnreachable
// lists a directory as JSON entries, refuses a path outside the
// workspace, and reports an unreachable workspace with exit 3 (ADR 0027).
func TestWorkspaceList_PrintsEntriesAndSeparatesRefusalFromUnreachable(t *testing.T) {
	addr, dir := startWorkspace(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))

	run := runOp(t, "", "list", ".", "--workspace-addr", addr)
	require.Equal(t, 0, run.status, "stderr: %s", run.stderr)
	assert.JSONEq(t, `{"entries":[{"name":"a.txt","dir":false},{"name":"sub","dir":true}]}`, run.stdout)

	requireRefused(t, runOp(t, "", "list", "..", "--workspace-addr", addr), "InvalidArgument")

	requireUnreachable(t, "list", ".")
}

// execResult is the exec subcommand's JSON result.
type execResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	TimedOut bool   `json:"timed_out"`
}

// runExec runs an exec that must succeed and decodes its result. A host
// that refuses the empty network namespace makes the workspace refuse
// every exec the CLI sends, which says nothing about the CLI, so the
// test skips unless SHERLOCK_REQUIRE_USERNS says the host must admit it.
func runExec(t *testing.T, stdin string, args ...string) execResult {
	t.Helper()
	run := runOp(t, stdin, append([]string{"exec"}, args...)...)
	if run.status == 2 && strings.Contains(run.stderr, shell.ErrAgentUIDUnavailable.Error()) {
		if os.Getenv("SHERLOCK_REQUIRE_USERNS") != "" {
			t.Fatalf("SHERLOCK_REQUIRE_USERNS is set but this host refuses the isolated exec: %s", run.stderr)
		}
		t.Skipf("this host cannot provide the isolation every CLI exec asks for: %s", run.stderr)
	}
	require.Equal(t, 0, run.status, "stderr: %s", run.stderr)
	var got execResult
	require.NoError(t, json.Unmarshal([]byte(run.stdout), &got))
	return got
}

// TestWorkspaceExec_ReturnsTheCommandsResultAndSeparatesRefusalFromUnreachable
// runs a command in the workspace through the shell and as an argv,
// forwards stdin only when asked, reports the command's own exit code as
// a result with exit 0, refuses an executable the workspace cannot
// start with exit 2, and reports an unreachable workspace with exit 3
// (ADR 0027).
func TestWorkspaceExec_ReturnsTheCommandsResultAndSeparatesRefusalFromUnreachable(t *testing.T) {
	addr, dir := startWorkspace(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seeded"), 0o644))
	at := []string{"--workspace-addr", addr}

	got := runExec(t, "", append(at, "cat seed.txt; echo; echo oops >&2; exit 3")...)
	assert.Equal(t, execResult{Stdout: "seeded\n", Stderr: "oops\n", ExitCode: 3}, got,
		"the command runs in the workspace directory, and its exit code is a result")

	got = runExec(t, "", append(at, "printf", "%s|", "a b", "c")...)
	assert.Equal(t, "a b|c|", got.Stdout, "more than one positional runs as an argv, without a shell")

	got = runExec(t, "piped", append(at, "--stdin", "cat")...)
	assert.Equal(t, "piped", got.Stdout, "--stdin forwards standard input")
	got = runExec(t, "piped", append(at, "cat")...)
	assert.Empty(t, got.Stdout, "without --stdin nothing is forwarded")

	refused := requireRefused(t, runOp(t, "", append([]string{"exec"}, append(at, "/nonexistent/program", "arg")...)...), "Internal")
	assert.Contains(t, refused.Message, "/nonexistent/program")

	requireUnreachable(t, "exec", "true")
}

// TestWorkspaceExec_RunsWithoutNetwork pins the isolation the CLI always
// asks for: the child's network namespace holds only loopback. The
// control is this host's own namespace, which holds another interface.
func TestWorkspaceExec_RunsWithoutNetwork(t *testing.T) {
	hostNet, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		t.Skipf("no /proc/net/dev on this host: %v", err)
	}
	require.Greater(t, countInterfaces(string(hostNet)), 1,
		"control: this host's namespace must hold an interface besides loopback")

	addr, _ := startWorkspace(t)
	got := runExec(t, "", "--workspace-addr", addr, "cat /proc/net/dev")
	assert.Equal(t, 1, countInterfaces(got.Stdout), "the child must see loopback alone: %q", got.Stdout)
	assert.Contains(t, got.Stdout, "lo:")
}

// countInterfaces counts the interface lines of a /proc/net/dev listing.
func countInterfaces(listing string) int {
	n := 0
	for _, line := range strings.Split(listing, "\n") {
		if strings.Contains(line, ":") && !strings.Contains(line, "|") {
			n++
		}
	}
	return n
}

// TestWorkspaceOperations_UsageFailsBeforeAnyCall reports an argument
// the CLI refuses with exit 1 and the usage class. The address is one
// nothing listens on, so a call would have reported exit 3 instead.
func TestWorkspaceOperations_UsageFailsBeforeAnyCall(t *testing.T) {
	addr := unusedAddr(t)
	for _, args := range [][]string{
		{"read", "--workspace-addr", addr},
		{"read", "a", "b", "--workspace-addr", addr},
		{"exec", "--workspace-addr", addr},
		{"list", ".", "--no-such-flag", "--workspace-addr", addr},
		{"list", ".", "--config", "agent.yaml", "--workspace-addr", addr},
	} {
		run := runOp(t, "", args...)
		assert.Equal(t, 1, run.status, "%v", args)
		assert.Equal(t, classUsage, run.failure(t).Class, "%v", args)
	}
}

// TestWorkspaceCommand_RefusesAPositionalArgument keeps a mistyped
// operation from starting a server: `sherlock workspace raed` fails
// before binding (ADR 0027). The control is `read`, which is routed to
// the operation and fails there instead.
func TestWorkspaceCommand_RefusesAPositionalArgument(t *testing.T) {
	addr := unusedAddr(t)
	configPath := filepath.Join(t.TempDir(), "workspace.yaml")
	require.NoError(t, os.WriteFile(configPath, nil, 0o600))
	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"workspace", "raed", "--config", configPath, "--addr", addr,
		"--workspace", filepath.Join(t.TempDir(), "workspace"), "--exec-uid", strconv.Itoa(os.Geteuid())})
	// A server that did start would serve until this deadline and then
	// return nil, so the refusal is the only way to get an error.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := root.ExecuteContext(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown command "raed" for "sherlock workspace"`)
	_, dialErr := net.DialTimeout("tcp", addr, 100*time.Millisecond)
	assert.Error(t, dialErr, "nothing may be listening at the address")

	run := runOp(t, "", "read", "x", "--workspace-addr", addr)
	assert.Equal(t, 3, run.status, "control: read is the operation, and reaches no listener")
}
