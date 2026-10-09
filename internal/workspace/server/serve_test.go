package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/garamsh/sherlock/internal/workspace"
	"github.com/garamsh/sherlock/internal/workspace/server"
	"github.com/garamsh/sherlock/internal/workspace/shell"
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

	opts := server.ServeOptions{
		Addr: addr,
		Dir:  filepath.Join(t.TempDir(), "workspace"),
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
}

// TestServe_WarnsOnSingleAccountShape pins the startup warning for the
// unsupported single-account shape (ADR 0008, #1003): an unprivileged
// workspace whose exec-uid is its own uid warns before serving, and one
// whose exec-uid is another uid serves without that warning.
func TestServe_WarnsOnSingleAccountShape(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the shape is unprivileged; a root run cannot take it")
	}
	tests := []struct {
		name     string
		execUID  int
		wantWarn bool
	}{
		{name: "exec-uid is the workspace's own uid", execUID: os.Geteuid(), wantWarn: true},
		{name: "exec-uid is another uid", execUID: os.Geteuid() + 1, wantWarn: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records := serveUntilListening(t, server.ServeOptions{
				Dir:         filepath.Join(t.TempDir(), "workspace"),
				ExecUID:     tt.execUID,
				ExecTimeout: 30 * time.Second,
			})

			var listening, warned bool
			for _, r := range records {
				if r["msg"] == "workspace listening" {
					listening = true
				}
				msg, _ := r["msg"].(string)
				if r["level"] == "WARN" && strings.Contains(msg, "single-account shape is not supported") {
					warned = true
					assert.Contains(t, msg, "deployment.md §Volumes and ownership",
						"the warning points to where the supported shape is stated")
				}
			}
			require.True(t, listening, "the workspace reached serving, so the absence of a warning is not a failed start")
			assert.Equal(t, tt.wantWarn, warned)
		})
	}
}

// serveUntilListening runs Serve on a free loopback address until its
// listener accepts, stops it, and returns the JSON log records it wrote.
func serveUntilListening(t *testing.T, opts server.ServeOptions) []map[string]any {
	t.Helper()
	listen, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	opts.Addr = listen.Addr().String()
	require.NoError(t, listen.Close())

	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, opts, slog.New(slog.NewJSONHandler(&logs, nil))) }()
	waitForWorkspaceListener(t, opts.Addr)
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("workspace did not shut down on context cancellation")
	}

	var records []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
		var r map[string]any
		require.NoError(t, json.Unmarshal(line, &r))
		records = append(records, r)
	}
	return records
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
