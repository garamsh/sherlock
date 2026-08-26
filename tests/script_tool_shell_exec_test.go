package tests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/tool"
	"github.com/garamsh/gagent/internal/workspace/shell"
)

// TestScriptTool_ShellExec_Integration drops the shell_exec tool
// into a tmpdir, wires it to a stub workspace gRPC server whose
// fake executor replays a canned result, and asserts the tool
// forwards the command to the workspace and returns the executor's
// stdout/stderr/exit_code.
func TestScriptTool_ShellExec_Integration(t *testing.T) {
	root := t.TempDir()
	hash := installTool(t, root, "shell_exec")

	addr, _, exec, stop := startFakeWorkspace(t)
	defer stop()
	exec.result = shell.Result{
		Stdout:   "hello from exec\n",
		Stderr:   "",
		ExitCode: 0,
		TimedOut: false,
	}
	t.Setenv("GAGENT_WORKSPACE_ADDR", addr)

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"shell_exec": hash},
		Required: []string{"shell_exec"},
	})
	require.NoError(t, err)

	out, err := reg.Call(context.Background(), "shell_exec",
		json.RawMessage(`{"command":"echo hello from exec"}`))
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(out, &body))
	assert.Equal(t, "hello from exec\n", body["stdout"])
	assert.EqualValues(t, 0, body["exit_code"])
	assert.Equal(t, false, body["timed_out"])
	assert.Equal(t, "echo hello from exec", exec.req.Command,
		"the script forwards the command to the workspace's Exec RPC")
}

// TestScriptTool_ShellExec_TimeoutSurfacesTrue covers the timeout
// path: the executor reports timed_out=true, the script returns it
// as the timed_out field, and the registry sees the result.
func TestScriptTool_ShellExec_TimeoutSurfacesTrue(t *testing.T) {
	root := t.TempDir()
	hash := installTool(t, root, "shell_exec")

	addr, _, exec, stop := startFakeWorkspace(t)
	defer stop()
	exec.result = shell.Result{
		Stdout:   "partial",
		Stderr:   "killed",
		ExitCode: 137,
		TimedOut: true,
	}
	t.Setenv("GAGENT_WORKSPACE_ADDR", addr)

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"shell_exec": hash},
		Required: []string{"shell_exec"},
	})
	require.NoError(t, err)

	out, err := reg.Call(context.Background(), "shell_exec",
		json.RawMessage(`{"command":"sleep 9999","timeout_nanos":1000000}`))
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(out, &body))
	assert.Equal(t, true, body["timed_out"])
	assert.EqualValues(t, 137, body["exit_code"])
	assert.Equal(t, int64(1000000), exec.req.Timeout.Nanoseconds(),
		"the script forwards timeout_nanos to the workspace's Exec RPC")
}

// TestScriptTool_ShellExec_InputRejected pins the boundary: a
// missing command is rejected before the script runs.
func TestScriptTool_ShellExec_InputRejected(t *testing.T) {
	root := t.TempDir()
	hash := installTool(t, root, "shell_exec")
	addr, _, _, stop := startFakeWorkspace(t)
	defer stop()
	t.Setenv("GAGENT_WORKSPACE_ADDR", addr)

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"shell_exec": hash},
		Required: []string{"shell_exec"},
	})
	require.NoError(t, err)

	_, err = reg.Call(context.Background(), "shell_exec",
		json.RawMessage(`{"timeout_nanos":1000}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrValidation)
}

// TestScriptTool_ShellExec_NullTimeoutAccepted pins the boundary fix
// from issue #319: the sanitizer promotes `timeout_nanos` into the
// required list with a nullable type, so the runtime's schema
// validation must accept `timeout_nanos: null`. The script's run()
// treats 0 as "use the workspace default" so the call completes.
func TestScriptTool_ShellExec_NullTimeoutAccepted(t *testing.T) {
	root := t.TempDir()
	hash := installTool(t, root, "shell_exec")

	addr, _, exec, stop := startFakeWorkspace(t)
	defer stop()
	exec.result = shell.Result{
		Stdout:   "ok\n",
		Stderr:   "",
		ExitCode: 0,
		TimedOut: false,
	}
	t.Setenv("GAGENT_WORKSPACE_ADDR", addr)

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"shell_exec": hash},
		Required: []string{"shell_exec"},
	})
	require.NoError(t, err)

	out, err := reg.Call(context.Background(), "shell_exec",
		json.RawMessage(`{"command":"echo ok","timeout_nanos":null}`))
	require.NoError(t, err, "null timeout must pass schema validation; the script uses the workspace default")
	var body map[string]any
	require.NoError(t, json.Unmarshal(out, &body))
	assert.Equal(t, "ok\n", body["stdout"])
	assert.EqualValues(t, 0, body["exit_code"])
}

// The two halves of the bound tools/shell_exec states on its Exec RPC:
// the command timeout it falls back to, and the margin it adds on top
// of whichever command timeout applies. The tool is a main package and
// cannot be imported, so the values are restated here — changing
// either there without changing it here fails these tests, which is
// what keeps deployment.md's requirement honest.
const (
	shellExecWorkspaceTimeout = 30 * time.Second
	shellExecDispatchMargin   = 5 * time.Second
)

// TestScriptTool_ShellExec_StalledWorkspaceRefused covers issue #726: a
// workspace that accepts the connection and then answers nothing is
// refused within the tool's own bound instead of parking the caller.
// The answering workspace in the same test is the control — it takes
// the same tool, input and wiring, so what the second half measures is
// the stall and not a peer that refused or closed. The per-call
// timeout is what the tool bounds against, so the call carries one.
func TestScriptTool_ShellExec_StalledWorkspaceRefused(t *testing.T) {
	root := t.TempDir()
	hash := installTool(t, root, "shell_exec")

	answering, _, exec, stop := startFakeWorkspace(t)
	defer stop()
	exec.result = shell.Result{Stdout: "hello from exec\n"}
	stalledAddr, stalled, stopStalled := startStalledWorkspace(t)
	defer stopStalled()

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"shell_exec": hash},
		Required: []string{"shell_exec"},
	})
	require.NoError(t, err)
	const commandTimeout = time.Millisecond
	call := json.RawMessage(`{"command":"echo hello from exec","timeout_nanos":1000000}`)

	t.Setenv("GAGENT_WORKSPACE_ADDR", answering)
	out, err := reg.Call(context.Background(), "shell_exec", call)
	require.NoError(t, err, "control: a workspace that answers accepts this exact call")
	assert.Contains(t, string(out), "hello from exec")

	t.Setenv("GAGENT_WORKSPACE_ADDR", stalledAddr)
	start := time.Now()
	_, err = reg.Call(context.Background(), "shell_exec", call)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrExecution)
	assert.GreaterOrEqual(t, elapsed, commandTimeout+shellExecDispatchMargin,
		"the tool waits its bound out: a peer that refused or closed would have failed fast")
	assert.Less(t, elapsed, commandTimeout+shellExecDispatchMargin+10*time.Second,
		"and then refuses, rather than parking the caller")
	assert.InDelta(t, (commandTimeout + shellExecDispatchMargin).Seconds(),
		stalled.observedDeadline().Seconds(), 2,
		"the bound tracks the command's own timeout, not one number for every call")
}

// TestScriptTool_ShellExec_DeadlineCoversWorkspaceDefault pins the
// other half of the bound: a call carrying no per-call timeout is run
// by the workspace under its own exec-timeout, which this tool cannot
// read, so the deadline it sets covers that default and the
// workspace's dispatch on top of it. Below that the tool would abandon
// commands the workspace is still running.
func TestScriptTool_ShellExec_DeadlineCoversWorkspaceDefault(t *testing.T) {
	root := t.TempDir()
	hash := installTool(t, root, "shell_exec")

	addr, _, exec, stop := startFakeWorkspace(t)
	defer stop()
	exec.result = shell.Result{Stdout: "ok\n"}
	t.Setenv("GAGENT_WORKSPACE_ADDR", addr)

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"shell_exec": hash},
		Required: []string{"shell_exec"},
	})
	require.NoError(t, err)

	_, err = reg.Call(context.Background(), "shell_exec",
		json.RawMessage(`{"command":"echo ok"}`))
	require.NoError(t, err)
	assert.Zero(t, exec.req.Timeout,
		"no per-call timeout reaches the executor, so the workspace applies exec-timeout")
	assert.InDelta(t, (shellExecWorkspaceTimeout + shellExecDispatchMargin).Seconds(),
		exec.deadline.Seconds(), 2,
		"the deadline covers the workspace's own exec-timeout default plus its dispatch")
}
