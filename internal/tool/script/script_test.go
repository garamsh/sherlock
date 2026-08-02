package script_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/shell"
	"github.com/garamsh/gagent/internal/tool"
	"github.com/garamsh/gagent/internal/tool/script"
)

// directExecutor is the test double for shell.Executor: it runs the
// request as the test user without the privilege drop, mirroring the
// ProcessExecutor result contract (timeout, exit code, captured
// streams), so the adapter's output contract is exercised without root.
type directExecutor struct{}

func (directExecutor) Run(ctx context.Context, req shell.Request) (shell.Result, error) {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = script.DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, req.Command, req.Args...)
	if req.Workdir != "" {
		cmd.Dir = req.Workdir
	}
	cmd.Env = append(tool.ChildEnv(), req.Env...)
	cmd.Stdin = bytes.NewReader(req.Stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := shell.Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if err == nil {
		return res, nil
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		res.TimedOut = true
		return res, shell.ErrTimedOut
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, nil
	}
	return res, err
}

// unavailableExecutor fails every run the way the platform does when it
// cannot drop privileges.
type unavailableExecutor struct{}

func (unavailableExecutor) Run(context.Context, shell.Request) (shell.Result, error) {
	return shell.Result{}, shell.ErrAgentUIDUnavailable
}

func TestToolScript_Accessors(t *testing.T) {
	ts := script.New(script.Spec{
		Name:         "echo",
		Description:  "echoes stdin",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"string"}`),
	}, directExecutor{})

	assert.Equal(t, "echo", ts.Name())
	assert.Equal(t, "echoes stdin", ts.Description())
	assert.JSONEq(t, `{"type":"object"}`, string(ts.InputSchema()))
	assert.JSONEq(t, `{"type":"string"}`, string(ts.OutputSchema()))
}

func TestToolScript_Call_RoundTrip(t *testing.T) {
	ts := script.New(script.Spec{
		Name: "echo",
		Exec: script.ExecSpec{
			Command:      []string{"sh", "-c", "cat"},
			StdoutFormat: "json",
		},
	}, directExecutor{})

	out, err := ts.Call(context.Background(), json.RawMessage(`{"hello":"world"}`))

	require.NoError(t, err)
	assert.JSONEq(t, `{"hello":"world"}`, string(out))
}

func TestToolScript_Call_RequiresCommand(t *testing.T) {
	ts := script.New(script.Spec{Name: "noop"}, directExecutor{})

	_, err := ts.Call(context.Background(), json.RawMessage(`{}`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), `tool "noop": exec.command is required`)
}

func TestToolScript_Call_RawStdout(t *testing.T) {
	ts := script.New(script.Spec{
		Name: "raw",
		Exec: script.ExecSpec{
			Command: []string{"sh", "-c", "printf 'not json'"},
		},
	}, directExecutor{})

	out, err := ts.Call(context.Background(), json.RawMessage(`{}`))

	require.NoError(t, err)
	assert.Equal(t, "not json", string(out))
}

func TestToolScript_Call_InvalidJSONOutput(t *testing.T) {
	ts := script.New(script.Spec{
		Name: "bad",
		Exec: script.ExecSpec{
			Command:      []string{"sh", "-c", "printf 'not json'"},
			StdoutFormat: "json",
		},
	}, directExecutor{})

	_, err := ts.Call(context.Background(), json.RawMessage(`{}`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), `tool "bad": invalid JSON output`)
}

func TestToolScript_Call_Workdir(t *testing.T) {
	dir := t.TempDir()
	ts := script.New(script.Spec{
		Name: "pwd",
		Exec: script.ExecSpec{
			Command: []string{"sh", "-c", "pwd -P"},
			Workdir: dir,
		},
	}, directExecutor{})

	out, err := ts.Call(context.Background(), json.RawMessage(`{}`))

	require.NoError(t, err)
	want, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	assert.Equal(t, want, strings.TrimSpace(string(out)))
}

func TestToolScript_Call_Env(t *testing.T) {
	ts := script.New(script.Spec{
		Name: "env",
		Exec: script.ExecSpec{
			Command: []string{"sh", "-c", `printf %s "$TOOL_SCRIPT_TEST_VAR"`},
			Env:     []string{"TOOL_SCRIPT_TEST_VAR=hello-env"},
		},
	}, directExecutor{})

	out, err := ts.Call(context.Background(), json.RawMessage(`{}`))

	require.NoError(t, err)
	assert.Equal(t, "hello-env", string(out))
}

func TestToolScript_Call_ScrubsPlatformEnv(t *testing.T) {
	t.Setenv("GAGENT_TEST_PLATFORM_SECRET", "hunter2")
	ts := script.New(script.Spec{
		Name: "env",
		Exec: script.ExecSpec{
			Command: []string{"env"},
		},
	}, directExecutor{})

	out, err := ts.Call(context.Background(), json.RawMessage(`{}`))

	require.NoError(t, err)
	assert.NotContains(t, string(out), "GAGENT_TEST_PLATFORM_SECRET",
		"platform variables must not reach script tool children (ADR 0004)")
	assert.Contains(t, string(out), "PATH=", "the minimal basics still reach the child")
}

func TestToolScript_Call_Timeout(t *testing.T) {
	ts := script.New(script.Spec{
		Name: "slow",
		Exec: script.ExecSpec{
			// exec replaces the shell so the kill reaches sleep directly;
			// an orphaned grandchild would hold the output pipes open.
			Command: []string{"sh", "-c", "exec sleep 3"},
			Timeout: 100 * time.Millisecond,
		},
	}, directExecutor{})

	start := time.Now()
	_, err := ts.Call(context.Background(), json.RawMessage(`{}`))
	elapsed := time.Since(start)

	require.ErrorIs(t, err, shell.ErrTimedOut)
	assert.Less(t, elapsed, 2*time.Second, "the command must be killed at the timeout, not run to completion")
}

func TestToolScript_Call_StderrInError(t *testing.T) {
	ts := script.New(script.Spec{
		Name: "fail",
		Exec: script.ExecSpec{
			Command: []string{"sh", "-c", "echo oops >&2; exit 1"},
		},
	}, directExecutor{})

	_, err := ts.Call(context.Background(), json.RawMessage(`{}`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), `tool "fail"`)
	assert.Contains(t, err.Error(), "oops", "stderr is included in the error message")
}

func TestToolScript_Call_AgentUIDUnavailable(t *testing.T) {
	ts := script.New(script.Spec{
		Name: "wordcount",
		Exec: script.ExecSpec{Command: []string{"wordcount"}},
	}, unavailableExecutor{})

	_, err := ts.Call(context.Background(), json.RawMessage(`{"text":"hi"}`))

	require.ErrorIs(t, err, shell.ErrAgentUIDUnavailable,
		"ADR 0014: no silent platform-UID run — the call fails closed")
	assert.Contains(t, err.Error(), "agent_uid unavailable")
}
