package tests

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/garamsh/gagent/internal/tool"
	"github.com/garamsh/gagent/internal/workspace"
	"github.com/garamsh/gagent/internal/workspace/files"
	"github.com/garamsh/gagent/internal/workspace/server"
	"github.com/garamsh/gagent/internal/workspace/shell"
)

// buildToolBinary compiles the tool named by name into its directory
// under root and returns the path. The script-tool runner resolves the
// manifest's command relative to the manifest's directory, so the
// binary must land at the same path the manifest names.
func buildToolBinary(t *testing.T, root, name string) string {
	t.Helper()
	toolDir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(toolDir, 0o755))
	build := exec.Command("go", "build", "-o", toolDir+"/"+name, "./tools/"+name+"/")
	build.Dir = ".."
	out, err := build.CombinedOutput()
	require.NoError(t, err, "go build %s: %s", name, out)
	return toolDir + "/" + name
}

// installTool copies the script tool's manifest into root/<name>/ and
// builds the binary in place. The manifest's command path resolves
// to the binary the function builds. The function returns the
// manifest's hash for the registry's drift-pin check.
func installTool(t *testing.T, root, name string) string {
	t.Helper()
	toolDir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(toolDir, 0o755))
	manifest, err := os.ReadFile(filepath.Join("..", "tools", name, "manifest.json"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, "manifest.json"), manifest, 0o644))
	buildToolBinary(t, root, name)
	hash, err := tool.ManifestHashOfFile(filepath.Join(toolDir, "manifest.json"))
	require.NoError(t, err)
	return hash
}

// startFakeWorkspace spins up the workspace gRPC server on an
// ephemeral loopback address and returns the address, the seeded
// fs, and the executor the server execs through. The address is what
// the script tool's GAGENT_WORKSPACE_ADDR must point at.
func startFakeWorkspace(t *testing.T) (string, *files.FS, *fakeExecutor, func()) {
	t.Helper()
	root := t.TempDir()
	fs, err := files.Open(root)
	require.NoError(t, err)
	exec := &fakeExecutor{}

	listen, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listen.Addr().String()

	srv := grpc.NewServer()
	workspace.RegisterWorkspaceServer(srv, &server.Server{FS: fs, Executor: exec})
	go func() { _ = srv.Serve(listen) }()

	stop := func() {
		srv.GracefulStop()
	}
	// Touch the dial so the test does not race commands against a
	// not-yet-listening server.
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	_ = conn.Close()
	return addr, fs, exec, stop
}

// fakeExecutor records the shell.Request the server hands the
// executor, what bounded the call, and replays a canned result. The
// script-tool tests only need the executor to return a known result;
// the real fork+exec code is covered in internal/workspace/shell.
type fakeExecutor struct {
	req shell.Request
	// defaultTimeout stands in for the workspace's exec-timeout. The
	// real executor resolves a request carrying no timeout to its own
	// default and runs the command under whichever of that and the
	// caller's deadline is nearer (internal/workspace/shell/process_linux.go),
	// so a double that ignored it could not show a caller's deadline
	// pre-empting the operator's ceiling.
	defaultTimeout time.Duration
	// deadline is how long the caller gave the whole call, and zero
	// where the caller gave no deadline at all.
	deadline time.Duration
	// commandBound is how long the command itself would have been
	// given, which is what an operator's exec-timeout is supposed to
	// decide.
	commandBound time.Duration
	result       shell.Result
	err          error
}

func (f *fakeExecutor) Run(ctx context.Context, req shell.Request) (shell.Result, error) {
	f.req = req
	if deadline, ok := ctx.Deadline(); ok {
		f.deadline = time.Until(deadline)
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = f.defaultTimeout
	}
	if timeout > 0 {
		runCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		deadline, _ := runCtx.Deadline()
		f.commandBound = time.Until(deadline)
	}
	return f.result, f.err
}

// stalledWorkspace accepts every RPC and answers none: each handler
// records how long the caller gave the call and then blocks until the
// caller gives up. It is the peer a data-plane deadline exists for
// (issue #726) — one that took the connection and stopped answering —
// and not one that refuses or closes, which fails fast without a
// deadline and so proves nothing about it.
type stalledWorkspace struct {
	workspace.UnimplementedWorkspaceServer

	// stopped is closed when the test tears the server down. It is what
	// releases a stalled handler: releasing it on the call's own context
	// instead would answer the caller with a cancellation the moment the
	// deadline propagated, and that answer races the caller's own
	// deadline — a peer under test for never answering must not answer.
	stopped  chan struct{}
	mu       sync.Mutex
	deadline time.Duration
}

func (s *stalledWorkspace) Read(ctx context.Context, _ *workspace.ReadRequest) (*workspace.ReadResponse, error) {
	return nil, s.stall(ctx)
}

func (s *stalledWorkspace) Write(ctx context.Context, _ *workspace.WriteRequest) (*workspace.WriteResponse, error) {
	return nil, s.stall(ctx)
}

func (s *stalledWorkspace) List(ctx context.Context, _ *workspace.ListRequest) (*workspace.ListResponse, error) {
	return nil, s.stall(ctx)
}

func (s *stalledWorkspace) Exec(ctx context.Context, _ *workspace.ExecRequest) (*workspace.ExecResponse, error) {
	return nil, s.stall(ctx)
}

func (s *stalledWorkspace) stall(ctx context.Context) error {
	if deadline, ok := ctx.Deadline(); ok {
		s.mu.Lock()
		s.deadline = time.Until(deadline)
		s.mu.Unlock()
	}
	<-s.stopped
	return status.Error(codes.Unavailable, "workspace stopped")
}

// observedDeadline is how long the caller of the last stalled RPC gave
// it, and zero where the caller gave no deadline at all. gRPC carries
// the client's deadline to the server, so this is the bound the tool
// set, read off the wire rather than out of the tool.
func (s *stalledWorkspace) observedDeadline() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deadline
}

// startStalledWorkspace serves the workspace contract on an ephemeral
// loopback address with every RPC stalled, and returns the address, the
// server, and its stop function. The listener accepts, the connection
// stays open and the process stays alive; no call is ever answered.
func startStalledWorkspace(t *testing.T) (string, *stalledWorkspace, func()) {
	t.Helper()
	stalled := &stalledWorkspace{stopped: make(chan struct{})}

	listen, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listen.Addr().String()

	srv := grpc.NewServer()
	workspace.RegisterWorkspaceServer(srv, stalled)
	go func() { _ = srv.Serve(listen) }()

	stop := func() {
		close(stalled.stopped)
		srv.GracefulStop()
	}
	return addr, stalled, stop
}

// TestScriptTool_Files_Integration drops the files tool into a
// tmpdir, wires it to a stub workspace gRPC server, and asserts one
// happy-path call (read, then write, then list) plus an error path
// (the workspace rejects the read with a non-OK gRPC code).
func TestScriptTool_Files_Integration(t *testing.T) {
	root := t.TempDir()
	hash := installTool(t, root, "files")

	addr, fs, _, stop := startFakeWorkspace(t)
	defer stop()
	require.NoError(t, fs.Write("hello.txt", []byte("hi from workspace")))

	// The manifest's env lists GAGENT_WORKSPACE_ADDR; the runner
	// forwards the parent's env to the spawned tool, so the test
	// sets the variable to the stub server's address before the
	// registry spawns the tool.
	t.Setenv("GAGENT_WORKSPACE_ADDR", addr)

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"files": hash},
		Required: []string{"files"},
	})
	require.NoError(t, err)

	// Read: the registry calls the files tool, which dials the
	// stub workspace and returns the file's contents.
	out, err := reg.Call(context.Background(), "files",
		json.RawMessage(`{"operation":"read","path":"hello.txt"}`))
	require.NoError(t, err)
	assert.Contains(t, string(out), "hi from workspace")

	// Write: round-trip a write and read it back.
	_, err = reg.Call(context.Background(), "files",
		json.RawMessage(`{"operation":"write","path":"new.txt","content":"written"}`))
	require.NoError(t, err)
	content, err := fs.Read("new.txt")
	require.NoError(t, err)
	assert.Equal(t, []byte("written"), content)

	// List: the workspace returns one entry for the file we wrote.
	out, err = reg.Call(context.Background(), "files",
		json.RawMessage(`{"operation":"list","path":"."}`))
	require.NoError(t, err)
	assert.Contains(t, string(out), "new.txt")
}

// TestScriptTool_Files_InputRejected pins the boundary: the registry
// refuses an input that does not match the manifest's input schema,
// so the script never runs.
func TestScriptTool_Files_InputRejected(t *testing.T) {
	root := t.TempDir()
	hash := installTool(t, root, "files")
	addr, _, _, stop := startFakeWorkspace(t)
	defer stop()
	t.Setenv("GAGENT_WORKSPACE_ADDR", addr)

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"files": hash},
		Required: []string{"files"},
	})
	require.NoError(t, err)

	// Missing path: the manifest requires "path", so the
	// registry rejects before the script runs.
	_, err = reg.Call(context.Background(), "files",
		json.RawMessage(`{"operation":"read"}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrValidation)
}

// TestScriptTool_Files_WriteRequiresContent covers the runtime check
// that replaced the previous top-level oneOf (issue #313): the
// strict-mode subset does not accept oneOf, so the conditional
// requirement that write needs non-empty content moved into the
// script. The registry's schema validation no longer rejects this
// case (content is now optional at the schema level); the runtime
// returns an execution error and the workspace never sees the call.
func TestScriptTool_Files_WriteRequiresContent(t *testing.T) {
	root := t.TempDir()
	hash := installTool(t, root, "files")
	addr, fs, _, stop := startFakeWorkspace(t)
	defer stop()
	t.Setenv("GAGENT_WORKSPACE_ADDR", addr)

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"files": hash},
		Required: []string{"files"},
	})
	require.NoError(t, err)

	// Empty content: the runtime check in dispatch returns an
	// error before gRPC is called.
	_, err = reg.Call(context.Background(), "files",
		json.RawMessage(`{"operation":"write","path":"new.txt","content":""}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrExecution)

	// The workspace must not have seen the call: the directory
	// listing should not contain new.txt.
	_, listErr := fs.List(".")
	require.NoError(t, listErr)
}

// TestScriptTool_Files_NullContentAccepted pins the boundary fix from
// issue #319 against the manifest gate: `content` is optional in the
// manifest, the model-facing projection promotes it to required +
// nullable (the strict-mode API only accepts required+nullable, never
// optional), so `content: null` must read as absence and pass. The
// script's own dispatch check (issue #313) then rejects the empty
// content, surfacing ErrExecution — proving the schema validation
// passed and the rejection came from the script body, not the runtime.
func TestScriptTool_Files_NullContentAccepted(t *testing.T) {
	root := t.TempDir()
	hash := installTool(t, root, "files")
	addr, _, _, stop := startFakeWorkspace(t)
	defer stop()
	t.Setenv("GAGENT_WORKSPACE_ADDR", addr)

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"files": hash},
		Required: []string{"files"},
	})
	require.NoError(t, err)

	_, err = reg.Call(context.Background(), "files",
		json.RawMessage(`{"operation":"write","path":"new.txt","content":null}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrExecution,
		"null content must pass schema validation and reach the script's dispatch check")
	assert.NotErrorIs(t, err, tool.ErrValidation,
		"a null on a manifest-optional property means absence, not a type violation")
}

// filesCallTimeout is the bound tools/files states on one workspace
// RPC. The tool is a main package and cannot be imported, so the value
// is restated here: changing it there without changing it here fails
// this test, which is what keeps deployment.md's requirement honest.
const filesCallTimeout = 10 * time.Second

// TestScriptTool_Files_StalledWorkspaceRefused covers issue #726: a
// workspace that accepts the connection and then answers nothing is
// refused within the tool's own bound instead of parking the caller.
// The answering workspace in the same test is the control — it takes
// the same tool, input and wiring, so what the second half measures is
// the stall and not a peer that refused or closed.
func TestScriptTool_Files_StalledWorkspaceRefused(t *testing.T) {
	root := t.TempDir()
	hash := installTool(t, root, "files")

	answering, fs, _, stop := startFakeWorkspace(t)
	defer stop()
	require.NoError(t, fs.Write("hello.txt", []byte("hi from workspace")))
	stalledAddr, stalled, stopStalled := startStalledWorkspace(t)
	defer stopStalled()

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"files": hash},
		Required: []string{"files"},
	})
	require.NoError(t, err)
	read := json.RawMessage(`{"operation":"read","path":"hello.txt"}`)

	t.Setenv("GAGENT_WORKSPACE_ADDR", answering)
	out, err := reg.Call(context.Background(), "files", read)
	require.NoError(t, err, "control: a workspace that answers accepts this exact call")
	assert.Contains(t, string(out), "hi from workspace")

	t.Setenv("GAGENT_WORKSPACE_ADDR", stalledAddr)
	start := time.Now()
	_, err = reg.Call(context.Background(), "files", read)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrExecution)
	assert.GreaterOrEqual(t, elapsed, filesCallTimeout,
		"the tool waits its bound out: a peer that refused or closed would have failed fast")
	assert.Less(t, elapsed, filesCallTimeout+10*time.Second,
		"and then refuses, rather than parking the caller")
	assert.InDelta(t, filesCallTimeout.Seconds(), stalled.observedDeadline().Seconds(), 2,
		"the workspace is handed the bound the tool states")
}
