package tests

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/garamsh/gagent/internal/workspace"
	"github.com/garamsh/gagent/internal/workspace/client"
)

// TestEndToEnd_WorkspaceServesContract drives "gagent workspace" as a
// subprocess — real binary, real gRPC, nothing mocked: the agent's
// tools (or a test driver) dial the workspace gRPC and call
// Read/Write/List (ADR 0017).
func TestEndToEnd_WorkspaceServesContract(t *testing.T) {
	binary := buildGagentBinary(t)
	workspaceDir := filepath.Join(t.TempDir(), "workspace")

	cmd, addr, exited := startWorkspaceBinary(t, binary, workspaceDir, emptyConfig(t, "workspace.yaml"))

	// Exercise the gRPC contract directly: write, read, list,
	// reject a traversal.
	ws, closeConn := dialWorkspace(t, addr)
	defer closeConn()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := ws.Write(ctx, &workspace.WriteRequest{Path: "sub/hello.txt", Content: []byte("hi workspace")})
	require.NoError(t, err)
	resp, err := ws.Read(ctx, &workspace.ReadRequest{Path: "sub/hello.txt"})
	require.NoError(t, err)
	assert.Equal(t, []byte("hi workspace"), resp.Content)

	content, err := os.ReadFile(filepath.Join(workspaceDir, "sub", "hello.txt"))
	require.NoError(t, err, "the write lands in the workspace's directory")
	assert.Equal(t, "hi workspace", string(content))

	_, err = ws.Read(ctx, &workspace.ReadRequest{Path: "../escape"})
	require.Error(t, err, "workspace confinement holds across the wire")

	listResp, err := ws.List(ctx, &workspace.ListRequest{Path: "."})
	require.NoError(t, err)
	require.Len(t, listResp.Entries, 1)
	assert.Equal(t, "sub", listResp.Entries[0].Name)
	assert.True(t, listResp.Entries[0].Dir)

	// SIGTERM triggers the binary's graceful shutdown.
	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	select {
	case err := <-exited:
		require.NoError(t, err, "workspace must exit cleanly on SIGTERM")
	case <-time.After(15 * time.Second):
		t.Fatal("workspace did not exit on SIGTERM")
	}
}

// TestEndToEnd_WorkspaceNonLoopbackFailsStartup runs the workspace with
// a non-loopback --addr: startup must fail with an error stating the
// loopback-only requirement — an unauthenticated gRPC listener is a
// remote shell into the workspace (ADR 0012, issue #209).
func TestEndToEnd_WorkspaceNonLoopbackFailsStartup(t *testing.T) {
	binary := buildGagentBinary(t)
	configPath := emptyConfig(t, "workspace.yaml")

	for _, addr := range []string{"0.0.0.0:8081", ":8081"} {
		cmd := exec.Command(binary, "workspace",
			"--config", configPath, "--addr", addr,
			"--workspace", filepath.Join(t.TempDir(), "workspace"))
		out, err := cmd.CombinedOutput()
		require.Error(t, err, "addr %q must fail startup", addr)
		assert.Contains(t, string(out), "loopback")
		assert.Contains(t, string(out), "unauthenticated")
	}
}

// startWorkspaceBinary starts "gagent workspace" as a subprocess on a
// free loopback port with workspace as the workspace directory and
// exec-uid set to the test process's own UID (single-account mode,
// ADR 0008), waits until it serves gRPC, and returns the process
// handle, the gRPC address, and a channel receiving the process's exit
// result. Test cleanup kills the process if it is still running.
func startWorkspaceBinary(t *testing.T, binary, workspaceDir, configPath string) (*exec.Cmd, string, <-chan error) {
	t.Helper()

	listen, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listen.Addr().String()
	require.NoError(t, listen.Close())

	aux := t.TempDir()
	logPath := filepath.Join(aux, "workspace.log")
	logFile, err := os.Create(logPath)
	require.NoError(t, err)
	t.Cleanup(func() { logFile.Close() })

	// Single-account mode: exec children run as the test process's own
	// UID, so the workspace needs no privilege to serve Exec.
	cmd := exec.Command(binary, "workspace",
		"--config", configPath, "--addr", addr, "--workspace", workspaceDir,
		"--exec-uid", strconv.Itoa(os.Geteuid()))
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	require.NoError(t, cmd.Start())
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			<-exited
		}
	})

	// Wait for the gRPC server to accept a List call. The listener
	// is unauthenticated loopback so a plaintext dial is correct.
	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case err := <-exited:
			logs, _ := os.ReadFile(logPath)
			t.Fatalf("workspace exited before serving: %v\nlogs:\n%s", err, logs)
		default:
		}
		ws, closeFn, err := client.New(context.Background(), addr)
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			_, err = ws.List(ctx, &workspace.ListRequest{Path: "."})
			cancel()
			closeFn()
			if err == nil {
				return cmd, addr, exited
			}
		}
		if time.Now().After(deadline) {
			logs, _ := os.ReadFile(logPath)
			t.Fatalf("workspace did not serve within 10s\nlogs:\n%s", logs)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// dialWorkspace dials addr over plaintext gRPC and returns the
// workspace client plus a close function.
func dialWorkspace(t *testing.T, addr string) (workspace.WorkspaceClient, func()) {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	return workspace.NewWorkspaceClient(conn), func() { _ = conn.Close() }
}
