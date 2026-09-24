package server_test

import (
	"context"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/garamsh/gagent/internal/workspace"
	"github.com/garamsh/gagent/internal/workspace/server"
	"github.com/garamsh/gagent/internal/workspace/shell"
)

// TestMain routes the resource-limit verb the way the platform binary
// does: Serve starts a real executor, which execs the binary it runs as
// — this test binary — to cap each untrusted child (ADR 0008).
func TestMain(m *testing.M) {
	shell.ShimIfRequested()
	os.Exit(m.Run())
}

// TestServe_ContractEndToEnd runs the workspace in-process and
// drives it through the gRPC contract: the workspace exposes
// Read/Write/List and rejects a path that escapes the workspace (ADR 0017).
func TestServe_ContractEndToEnd(t *testing.T) {
	listen, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listen.Addr().String()
	require.NoError(t, listen.Close())

	t.Setenv("QA_WORKSPACE_TOKEN", "operator-marker")
	t.Setenv("MINIMAX_KEY", "model-marker")
	opts := server.ServeOptions{
		ExecEnv: []string{"QA_WORKSPACE_TOKEN"},
		Addr:    addr,
		Dir:     filepath.Join(t.TempDir(), "workspace"),
		// Single-account mode: exec children run as the test process's
		// own UID, so no privilege is needed.
		ExecUID:     os.Geteuid(),
		ExecTimeout: 30 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, opts, slog.New(slog.DiscardHandler)) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Fatal("workspace did not shut down on context cancellation")
		}
	})

	waitForWorkspaceListener(t, addr)

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

	// Exec crosses the same wire: the command runs in the workspace
	// directory and sees the files the contract serves (ADR 0017).
	execResp, err := client.Exec(ctxRPC, &workspace.ExecRequest{Command: "ls"})
	require.NoError(t, err)
	assert.Zero(t, execResp.GetExitCode())
	assert.Contains(t, execResp.GetStdout(), "sub",
		"exec children work in the workspace directory by default")
	execResp, err = client.Exec(ctxRPC, &workspace.ExecRequest{
		Command: `printf '%s|%s' "$QA_WORKSPACE_TOKEN" "${MINIMAX_KEY-unset}"`,
		Env:     []string{"QA_WORKSPACE_TOKEN=caller-marker"},
	})
	require.NoError(t, err)
	assert.Zero(t, execResp.GetExitCode())
	assert.Equal(t, "operator-marker|unset", execResp.GetStdout())
}

// waitForWorkspaceListener polls until the workspace's listener accepts
// TCP connections, so the gRPC client never dials a server whose
// net.Listen has not run yet — grpc.NewClient is lazy and the first RPC
// would otherwise fail fast with connection refused (issue #270). Once
// the kernel accepts a probe connection the gRPC server is serving.
func waitForWorkspaceListener(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("workspace listener at %s did not accept connections within 5s: %v", addr, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
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

func TestServe_EnvironmentGrantFailures(t *testing.T) {
	for _, name := range []string{"INVALID=literal", "QA_UNSET_WORKSPACE_GRANT_840"} {
		t.Run(name, func(t *testing.T) {
			err := server.Serve(context.Background(), server.ServeOptions{ExecEnv: []string{name}}, slog.New(slog.DiscardHandler))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "exec-env")
			assert.NotContains(t, err.Error(), "literal")
		})
	}
}
