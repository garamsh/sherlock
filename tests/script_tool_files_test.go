package tests

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

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
// executor and replays a canned result. The script-tool tests only
// need the executor to return a known result; the real fork+exec
// code is covered in internal/workspace/shell.
type fakeExecutor struct {
	req    shell.Request
	result shell.Result
	err    error
}

func (f *fakeExecutor) Run(_ context.Context, req shell.Request) (shell.Result, error) {
	f.req = req
	return f.result, f.err
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
