package tests

import (
	"context"
	"encoding/json"
	"testing"

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
	t.Cleanup(func() { reg.Close() })

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
	t.Cleanup(func() { reg.Close() })

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
	t.Cleanup(func() { reg.Close() })

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
	t.Cleanup(func() { reg.Close() })

	out, err := reg.Call(context.Background(), "shell_exec",
		json.RawMessage(`{"command":"echo ok","timeout_nanos":null}`))
	require.NoError(t, err, "null timeout must pass schema validation; the script uses the workspace default")
	var body map[string]any
	require.NoError(t, json.Unmarshal(out, &body))
	assert.Equal(t, "ok\n", body["stdout"])
	assert.EqualValues(t, 0, body["exit_code"])
}
