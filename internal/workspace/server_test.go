package workspace_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/garamsh/gagent/internal/workspace"
	"github.com/garamsh/gagent/internal/workspace/files"
	"github.com/garamsh/gagent/internal/workspace/shell"
)

// fakeExecutor records the shell.Request the server hands the executor
// and replays a canned result; the executor's fork+exec implementation
// is covered in internal/workspace/shell.
type fakeExecutor struct {
	req    shell.Request
	result shell.Result
	err    error
}

func (f *fakeExecutor) Run(_ context.Context, req shell.Request) (shell.Result, error) {
	f.req = req
	return f.result, f.err
}

// newTestWorkspace spins up the workspace gRPC server on an ephemeral
// loopback address and returns the client, the seeded fs, the executor
// the server execs through, and a stop function. The server is backed
// by a fresh files.FS rooted in a per-test temp dir; the fs lets the
// test seed files before dialing.
func newTestWorkspace(t *testing.T) (workspace.WorkspaceClient, *files.FS, *fakeExecutor, func()) {
	t.Helper()
	root := t.TempDir()
	fs, err := files.Open(root)
	require.NoError(t, err)
	exec := &fakeExecutor{}

	listen, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := grpc.NewServer()
	workspace.RegisterWorkspaceServer(srv, &workspace.Server{FS: fs, Executor: exec})

	go func() { _ = srv.Serve(listen) }()

	conn, err := grpc.NewClient(
		listen.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	stop := func() {
		conn.Close()
		srv.GracefulStop()
	}
	return workspace.NewWorkspaceClient(conn), fs, exec, stop
}

func TestServer_Read(t *testing.T) {
	t.Run("existing path", func(t *testing.T) {
		client, fs, _, stop := newTestWorkspace(t)
		defer stop()
		require.NoError(t, fs.Write("hello.txt", []byte("hi")))

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		resp, err := client.Read(ctx, &workspace.ReadRequest{Path: "hello.txt"})
		require.NoError(t, err)
		assert.Equal(t, []byte("hi"), resp.GetContent())
	})

	t.Run("missing path", func(t *testing.T) {
		client, _, _, stop := newTestWorkspace(t)
		defer stop()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := client.Read(ctx, &workspace.ReadRequest{Path: "nope.txt"})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, "InvalidArgument", st.Code().String(),
			"missing paths surface as InvalidArgument, mirroring the existing server handler")
	})

	t.Run("escape rejected", func(t *testing.T) {
		client, _, _, stop := newTestWorkspace(t)
		defer stop()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := client.Read(ctx, &workspace.ReadRequest{Path: "../escape"})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, "InvalidArgument", st.Code().String())
		assert.Contains(t, st.Message(), "path escapes workspace")
	})
}

func TestServer_Write(t *testing.T) {
	client, _, _, stop := newTestWorkspace(t)
	defer stop()

	t.Run("valid path", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := client.Write(ctx, &workspace.WriteRequest{Path: "sub/dir/file.txt", Content: []byte("payload")})
		require.NoError(t, err)

		// Re-read via the same client to confirm the write hit disk.
		resp, err := client.Read(ctx, &workspace.ReadRequest{Path: "sub/dir/file.txt"})
		require.NoError(t, err)
		assert.Equal(t, []byte("payload"), resp.GetContent())
	})

	t.Run("invalid path", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := client.Write(ctx, &workspace.WriteRequest{Path: "../escape.txt", Content: []byte("x")})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, "InvalidArgument", st.Code().String())
		assert.Contains(t, st.Message(), "path escapes workspace")
	})
}

func TestServer_List(t *testing.T) {
	client, fs, _, stop := newTestWorkspace(t)
	defer stop()
	require.NoError(t, fs.Write("a.txt", []byte("a")))
	require.NoError(t, fs.Write("b.txt", []byte("b")))
	require.NoError(t, fs.Write("sub/c.txt", []byte("c")))

	t.Run("directory", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		resp, err := client.List(ctx, &workspace.ListRequest{Path: "."})
		require.NoError(t, err)

		names := make([]string, 0, len(resp.GetEntries()))
		dirs := make(map[string]bool)
		for _, e := range resp.GetEntries() {
			names = append(names, e.GetName())
			dirs[e.GetName()] = e.GetDir()
		}
		assert.ElementsMatch(t, []string{"a.txt", "b.txt", "sub"}, names)
		assert.True(t, dirs["sub"], "sub must report as a directory")
		assert.False(t, dirs["a.txt"], "files must report as not directories")
	})

	t.Run("subdirectory", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		resp, err := client.List(ctx, &workspace.ListRequest{Path: "sub"})
		require.NoError(t, err)
		require.Len(t, resp.GetEntries(), 1)
		assert.Equal(t, "c.txt", resp.GetEntries()[0].GetName())
	})

	t.Run("missing path", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := client.List(ctx, &workspace.ListRequest{Path: "nope"})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, "InvalidArgument", st.Code().String())
	})
}

func TestServer_Exec(t *testing.T) {
	t.Run("runs through the executor in the workspace directory", func(t *testing.T) {
		client, fs, exec, stop := newTestWorkspace(t)
		defer stop()
		exec.result = shell.Result{Stdout: "ok\n"}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		resp, err := client.Exec(ctx, &workspace.ExecRequest{
			Command:      "ls",
			TimeoutNanos: int64(5 * time.Second),
			NoNetwork:    true,
		})
		require.NoError(t, err)
		assert.Equal(t, "ok\n", resp.GetStdout())
		assert.False(t, resp.GetTimedOut())

		assert.Equal(t, "ls", exec.req.Command)
		assert.Equal(t, fs.Root(), exec.req.Workdir,
			"an empty workdir defaults to the workspace directory")
		assert.Equal(t, 5*time.Second, exec.req.Timeout)
		assert.True(t, exec.req.NoNetwork)
	})

	t.Run("timeout is a result, not an error", func(t *testing.T) {
		client, _, exec, stop := newTestWorkspace(t)
		defer stop()
		exec.result = shell.Result{Stdout: "partial"}
		exec.err = shell.ErrTimedOut

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		resp, err := client.Exec(ctx, &workspace.ExecRequest{Command: "sleep 60"})
		require.NoError(t, err, "a timed-out command is a normal outcome (ADR 0008)")
		assert.Equal(t, "partial", resp.GetStdout())
		assert.True(t, resp.GetTimedOut())
	})

	t.Run("executor failure surfaces as Internal", func(t *testing.T) {
		client, _, exec, stop := newTestWorkspace(t)
		defer stop()
		exec.err = errors.New("shell: agent_uid unavailable")

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := client.Exec(ctx, &workspace.ExecRequest{Command: "true"})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, "Internal", st.Code().String())
		assert.Contains(t, st.Message(), "agent_uid unavailable",
			"the fail-closed privilege drop crosses the wire")
	})
}
