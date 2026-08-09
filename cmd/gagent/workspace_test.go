package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/shell"
	"github.com/garamsh/gagent/internal/tool"
	"github.com/garamsh/gagent/internal/tool/script"
)

// newTestWorkspaceCommand returns the workspace command with the shared
// persistent flags merged the way command execution merges them, so
// resolveWorkspaceConfig can be exercised without running the server.
func newTestWorkspaceCommand(t *testing.T) *cobra.Command {
	t.Helper()
	// Isolate the test from a real XDG/home config.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := newRootCommand()
	cmd, _, err := root.Find([]string{"workspace"})
	require.NoError(t, err)
	cmd.Flags().AddFlagSet(root.PersistentFlags())
	return cmd
}

func TestResolveWorkspaceConfig_Defaults(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg, err := resolveWorkspaceConfig(newTestWorkspaceCommand(t))
	require.NoError(t, err)

	assert.Equal(t, defaultWorkspaceAddr, cfg.Addr)
	assert.Equal(t, defaultWorkspace, cfg.Workspace)
	assert.Equal(t, defaultExecTimeout, cfg.ExecTimeout)
	assert.Equal(t, shell.DefaultAgentUID, cfg.ExecUID,
		"the default exec-uid preserves the privileged-drop behavior")
	assert.Empty(t, cfg.Tools)
}

func TestResolveWorkspaceConfig_ConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	contents := "addr: \"127.0.0.1:9091\"\n" +
		"workspace: /srv/workspace\n" +
		"exec-timeout: 5s\n" +
		"exec-uid: 1000\n" +
		"tools:\n" +
		"  - name: wordcount\n" +
		"    description: Count words in a text.\n" +
		"    input_schema: schemas/wordcount.input.json\n" +
		"    output_schema: schemas/wordcount.output.json\n" +
		"    exec:\n" +
		"      command: [python3, tools/wordcount.py]\n" +
		"      stdout_format: json\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workspace.yaml"), []byte(contents), 0o600))

	cfg, err := resolveWorkspaceConfig(newTestWorkspaceCommand(t))
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:9091", cfg.Addr)
	assert.Equal(t, "/srv/workspace", cfg.Workspace)
	assert.Equal(t, 5*time.Second, cfg.ExecTimeout)
	assert.Equal(t, 1000, cfg.ExecUID)
	require.Len(t, cfg.Tools, 1)
	assert.Equal(t, "wordcount", cfg.Tools[0].Name)
	assert.Equal(t, []string{"python3", "tools/wordcount.py"}, cfg.Tools[0].Exec.Command)
}

func TestResolveWorkspaceConfig_EnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workspace.yaml"),
		[]byte("workspace: /from-file\n"), 0o600))
	t.Setenv("GAGENT_WORKSPACE", "/from-env")

	cfg, err := resolveWorkspaceConfig(newTestWorkspaceCommand(t))
	require.NoError(t, err)

	assert.Equal(t, "/from-env", cfg.Workspace)
}

func TestResolveWorkspaceConfig_FlagOverridesEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GAGENT_WORKSPACE", "/from-env")
	cmd := newTestWorkspaceCommand(t)
	require.NoError(t, cmd.Flags().Set("workspace", "/from-flag"))

	cfg, err := resolveWorkspaceConfig(cmd)
	require.NoError(t, err)

	assert.Equal(t, "/from-flag", cfg.Workspace)
}

func TestResolveWorkspaceConfig_XDGConfigFile(t *testing.T) {
	t.Chdir(t.TempDir())
	xdg := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(xdg, "gagent"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(xdg, "gagent", "workspace.yaml"),
		[]byte("workspace: /from-xdg\n"), 0o600))
	cmd := newTestWorkspaceCommand(t)
	t.Setenv("XDG_CONFIG_HOME", xdg)

	cfg, err := resolveWorkspaceConfig(cmd)
	require.NoError(t, err)

	assert.Equal(t, "/from-xdg", cfg.Workspace)
}

func TestResolveWorkspaceConfig_ConfigFlag(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	// A cwd workspace.yaml exists but the flag path wins.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workspace.yaml"),
		[]byte("workspace: /from-cwd\n"), 0o600))
	custom := filepath.Join(t.TempDir(), "custom.yaml")
	require.NoError(t, os.WriteFile(custom, []byte("workspace: /from-flag\n"), 0o600))

	cmd := newTestWorkspaceCommand(t)
	require.NoError(t, cmd.Flags().Set("config", custom))
	cfg, err := resolveWorkspaceConfig(cmd)
	require.NoError(t, err)

	assert.Equal(t, "/from-flag", cfg.Workspace)
}

// TestServeWorkspace_ContractEndToEnd runs the workspace in-process and
// drives it through the contract client: the registry lists files,
// shell_exec, and the configured script tool, and the files tool
// round-trips a write/read while rejecting a path that escapes the
// workspace. Unprivileged runs report agent_uid unavailable from
// shell_exec, the documented fail-closed mode.
func TestServeWorkspace_ContractEndToEnd(t *testing.T) {
	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.json")
	require.NoError(t, os.WriteFile(schema, []byte(`{"type":"object"}`), 0o600))

	listen, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listen.Addr().String()
	require.NoError(t, listen.Close())

	cfg := WorkspaceConfig{
		Addr:        addr,
		Workspace:   filepath.Join(dir, "workspace"),
		ExecTimeout: 5 * time.Second,
		Tools: []script.Config{{
			Name:         "echo",
			Description:  "Echo the JSON input back.",
			InputSchema:  schema,
			OutputSchema: schema,
			Exec:         script.ExecConfig{Command: []string{"cat"}},
		}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveWorkspace(ctx, cfg) }()
	baseURL := "http://" + addr
	require.Eventually(t, func() bool {
		_, err := tool.Discover(context.Background(), baseURL)
		return err == nil
	}, 10*time.Second, 20*time.Millisecond, "the workspace must serve the contract")

	descs, err := tool.Discover(context.Background(), baseURL)
	require.NoError(t, err)
	names := make([]string, 0, len(descs))
	byName := make(map[string]tool.Descriptor, len(descs))
	for _, d := range descs {
		names = append(names, d.Name)
		byName[d.Name] = d
	}
	assert.Equal(t, []string{"echo", "files", "shell_exec"}, names)

	filesTool := tool.NewRemoteTool(byName["files"], baseURL)
	_, err = tool.Invoke(context.Background(), filesTool,
		json.RawMessage(`{"operation":"write","path":"sub/hello.txt","content":"hi"}`))
	require.NoError(t, err)
	out, err := tool.Invoke(context.Background(), filesTool,
		json.RawMessage(`{"operation":"read","path":"sub/hello.txt"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"content":"hi"}`, string(out))

	_, err = tool.Invoke(context.Background(), filesTool,
		json.RawMessage(`{"operation":"read","path":"../escape"}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrExecution)
	assert.Contains(t, err.Error(), "path escapes workspace",
		"workspace confinement holds across the wire")

	shellTool := tool.NewRemoteTool(byName["shell_exec"], baseURL)
	out, err = tool.Invoke(context.Background(), shellTool,
		json.RawMessage(`{"command":"echo hi"}`))
	if err != nil {
		// The wire carries the error kind, not the sentinel identity.
		assert.ErrorIs(t, err, tool.ErrExecution)
		assert.Contains(t, err.Error(), "agent_uid unavailable",
			"an unprivileged workspace fails shell_exec closed")
	} else {
		assert.Contains(t, string(out), "hi")
	}

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err, "context cancellation must shut the workspace down cleanly")
	case <-time.After(15 * time.Second):
		t.Fatal("workspace did not shut down on context cancellation")
	}
}

// TestServeWorkspace_NonLoopbackAddrFailsStartup pins the fail-closed
// posture: the contract listener is unauthenticated in this phase, so a
// non-loopback bind is a remote shell into the workspace and fails
// startup (ADR 0013).
func TestServeWorkspace_NonLoopbackAddrFailsStartup(t *testing.T) {
	dir := t.TempDir()
	cfg := WorkspaceConfig{
		Addr:        "0.0.0.0:8081",
		Workspace:   filepath.Join(dir, "workspace"),
		ExecTimeout: 5 * time.Second,
	}

	err := serveWorkspace(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loopback",
		"the startup error must state the loopback-only requirement")
	assert.Contains(t, err.Error(), "unauthenticated",
		"the startup error must state the reason")
}

func TestServeWorkspace_InvalidToolFailsStartup(t *testing.T) {
	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.json")
	require.NoError(t, os.WriteFile(schema, []byte(`{"type":"object"}`), 0o600))
	cfg := WorkspaceConfig{
		Addr:        "127.0.0.1:0",
		Workspace:   filepath.Join(dir, "workspace"),
		ExecTimeout: 5 * time.Second,
		Tools: []script.Config{{
			Name:         "files",
			Description:  "Collides with the built-in files tool.",
			InputSchema:  schema,
			OutputSchema: schema,
			Exec:         script.ExecConfig{Command: []string{"true"}},
		}},
	}

	err := serveWorkspace(context.Background(), cfg)
	require.ErrorIs(t, err, tool.ErrDuplicateTool,
		"a script tool named after a served built-in must fail startup")
	assert.Contains(t, err.Error(), `"files"`,
		"the startup error must name the offending tool")
}
