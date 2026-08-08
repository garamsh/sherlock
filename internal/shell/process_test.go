package shell_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/shell"
	"github.com/garamsh/gagent/internal/tool"
)

// fakeExecutor is the test double for the Executor interface. It records
// the last request and returns the canned result and error, so the Tool
// adapter can be exercised end-to-end without a real fork+exec.
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
		t.Skip("setuid path requires root; run with `sudo -E go test ./internal/shell/...` to exercise fork+exec")
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
	exec := shell.NewProcessExecutor(shell.DefaultAgentUID, 5*time.Second)

	_, err := exec.Run(context.Background(), shell.Request{Command: "echo hi"})

	require.ErrorIs(t, err, shell.ErrAgentUIDUnavailable)
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
		"platform variables must not reach shell_exec children (ADR 0003)")
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

func TestTool_InvalidInputRejectedAtBoundary(t *testing.T) {
	exec := &fakeExecutor{}
	toolImpl := shell.NewTool(exec)

	tests := []struct {
		name  string
		input string
	}{
		{name: "missing command", input: `{}`},
		{name: "empty command", input: `{"command":""}`},
		{name: "wrong field type", input: `{"command":42}`},
		{name: "additional field", input: `{"command":"ls","mode":"fast"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.Invoke(context.Background(), toolImpl, json.RawMessage(tt.input))
			require.Error(t, err)
			assert.ErrorIs(t, err, tool.ErrValidation)
			assert.Empty(t, exec.req.Command, "the executor must not run on invalid input")
		})
	}
}

func TestTool_DispatchesToExecutorAndMarshalsResult(t *testing.T) {
	exec := &fakeExecutor{result: shell.Result{
		Stdout:   "hi\n",
		Stderr:   "",
		ExitCode: 0,
	}}
	toolImpl := shell.NewTool(exec).WithDefaultTimeout(5 * time.Second)

	out, err := tool.Invoke(context.Background(), toolImpl,
		json.RawMessage(`{"command":"echo hi","timeout_seconds":5}`))
	require.NoError(t, err)

	assert.Equal(t, "echo hi", exec.req.Command, "command passes through unchanged")
	assert.Equal(t, 5*time.Second, exec.req.Timeout,
		"timeout_seconds overrides the tool default")
	assert.True(t, exec.req.NoNetwork,
		"shell_exec children always run in a fresh network namespace (ADR 0010)")
	assert.JSONEq(t, `{"stdout":"hi\n","stderr":"","exit_code":0,"timed_out":false}`, string(out))
}

func TestTool_PassesWorkspaceWorkdirToExecutor(t *testing.T) {
	exec := &fakeExecutor{}
	toolImpl := shell.NewTool(exec).WithWorkdir("/env/workspace")

	_, err := tool.Invoke(context.Background(), toolImpl,
		json.RawMessage(`{"command":"ls"}`))

	require.NoError(t, err)
	assert.Equal(t, "/env/workspace", exec.req.Workdir,
		"the tool runs every command in the wired working directory")
}

func TestTool_TimeoutSurfacesTimedOutResult(t *testing.T) {
	exec := &fakeExecutor{
		result: shell.Result{Stdout: "partial output\n", TimedOut: true},
		err:    shell.ErrTimedOut,
	}
	toolImpl := shell.NewTool(exec)

	out, err := tool.Invoke(context.Background(), toolImpl,
		json.RawMessage(`{"command":"sleep 60","timeout_seconds":2}`))

	require.NoError(t, err, "a timeout is a normal outcome per ADR 0008, not a tool failure")
	assert.JSONEq(t,
		`{"stdout":"partial output\n","stderr":"","exit_code":0,"timed_out":true}`,
		string(out))
}

func TestTool_ExecutorErrorSurfacesAsExecutionError(t *testing.T) {
	exec := &fakeExecutor{err: errors.New("boom")}
	toolImpl := shell.NewTool(exec)

	_, err := tool.Invoke(context.Background(), toolImpl,
		json.RawMessage(`{"command":"echo hi"}`))

	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrExecution)
	assert.ErrorContains(t, err, "boom")
}
