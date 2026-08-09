package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/garamsh/gagent/internal/workspace"
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
}

func TestResolveWorkspaceConfig_ConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	contents := "addr: \"127.0.0.1:9091\"\n" +
		"workspace: /srv/workspace\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workspace.yaml"), []byte(contents), 0o600))

	cfg, err := resolveWorkspaceConfig(newTestWorkspaceCommand(t))
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:9091", cfg.Addr)
	assert.Equal(t, "/srv/workspace", cfg.Workspace)
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
// drives it through the gRPC contract: the workspace exposes
// Read/Write/List and rejects a path that escapes the workspace (ADR 0018).
func TestServeWorkspace_ContractEndToEnd(t *testing.T) {
	listen, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listen.Addr().String()
	require.NoError(t, listen.Close())

	cfg := WorkspaceConfig{
		Addr:      addr,
		Workspace: filepath.Join(t.TempDir(), "workspace"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveWorkspace(ctx, cfg) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Fatal("workspace did not shut down on context cancellation")
		}
	})

	client, closeConn := dialWorkspace(t, addr)
	t.Cleanup(closeConn)

	ctxRPC, cancelRPC := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelRPC()

	_, err = client.Write(ctxRPC, &workspace.WriteRequest{Path: "sub/hello.txt", Content: []byte("hi")})
	require.NoError(t, err)
	resp, err := client.Read(ctxRPC, &workspace.ReadRequest{Path: "sub/hello.txt"})
	require.NoError(t, err)
	assert.Equal(t, []byte("hi"), resp.Content)

	// Confinement holds: a traversal is rejected (ADR 0005 — convenience
	// confinement, not a trust boundary, but enforced everywhere).
	_, err = client.Read(ctxRPC, &workspace.ReadRequest{Path: "../escape"})
	require.Error(t, err, "an out-of-workspace path must be rejected")

	listResp, err := client.List(ctxRPC, &workspace.ListRequest{Path: "."})
	require.NoError(t, err)
	require.Len(t, listResp.Entries, 1)
	assert.Equal(t, "sub", listResp.Entries[0].Name)
	assert.True(t, listResp.Entries[0].Dir)
}

// TestServeWorkspace_NonLoopbackAddrFailsStartup pins the fail-closed
// posture: the gRPC listener is unauthenticated in this phase, so a
// non-loopback bind is a remote shell into the workspace and fails
// startup (ADR 0013).
func TestServeWorkspace_NonLoopbackAddrFailsStartup(t *testing.T) {
	cfg := WorkspaceConfig{
		Addr:      "0.0.0.0:8081",
		Workspace: filepath.Join(t.TempDir(), "workspace"),
	}

	err := serveWorkspace(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loopback",
		"the startup error must state the loopback-only requirement")
	assert.Contains(t, err.Error(), "unauthenticated",
		"the startup error must state the reason")
}

// dialWorkspace builds a workspace gRPC client connected to addr. The
// listener is loopback-only and unauthenticated, so a plaintext
// connection is correct. The returned close function closes the
// underlying connection.
func dialWorkspace(t *testing.T, addr string) (workspace.WorkspaceClient, func()) {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	return workspace.NewWorkspaceClient(conn), func() { _ = conn.Close() }
}
