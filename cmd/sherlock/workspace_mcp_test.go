package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/workspace/shell"
)

// connectMCP starts `sherlock workspace mcp` for addr as a child over
// stdio, the way an MCP client starts a local server, and returns the
// client's session.
func connectMCP(t *testing.T, addr string) *mcp.ClientSession {
	t.Helper()
	self, err := os.Executable()
	require.NoError(t, err)
	server := exec.Command(self, "workspace", "mcp", "--workspace-addr", addr)
	server.Env = append(os.Environ(), runAsBinaryEnv+"=1")
	server.Stderr = os.Stderr

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "sherlock-test", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: server}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// callTool calls name with args and returns the result or the error.
func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) (*mcp.CallToolResult, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
}

// requireToolResult asserts a successful tool result and returns it.
func requireToolResult(t *testing.T, res *mcp.CallToolResult, err error) *mcp.CallToolResult {
	t.Helper()
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", resultText(res))
	return res
}

// resultText is the text of a result's first content block.
func resultText(res *mcp.CallToolResult) string {
	if res == nil || len(res.Content) == 0 {
		return ""
	}
	if text, ok := res.Content[0].(*mcp.TextContent); ok {
		return text.Text
	}
	return ""
}

// structuredJSON re-encodes a result's structured content for JSONEq.
func structuredJSON(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	out, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	return string(out)
}

// requireWorkspaceRefusal asserts exit 2's answer: a tool result marked
// as an error, carrying the CLI's failure object (ADR 0028 §5).
func requireWorkspaceRefusal(t *testing.T, res *mcp.CallToolResult, err error, code string) operationError {
	t.Helper()
	require.NoError(t, err, "a workspace refusal is a tool result, not a protocol error")
	require.True(t, res.IsError)
	var failure operationError
	require.NoError(t, json.Unmarshal([]byte(resultText(res)), &failure), "content: %q", resultText(res))
	assert.Equal(t, classWorkspace, failure.Class)
	assert.Equal(t, code, failure.Code)
	return failure
}

// requireRPCError asserts a JSON-RPC error with code, carrying the CLI's
// failure object of class as its data.
func requireRPCError(t *testing.T, res *mcp.CallToolResult, err error, code int64, class failureClass) {
	t.Helper()
	require.Error(t, err, "result: %+v", res)
	var wire *jsonrpc.Error
	require.True(t, errors.As(err, &wire), "a JSON-RPC error, got %T: %v", err, err)
	assert.Equal(t, code, wire.Code)
	var failure operationError
	require.NoError(t, json.Unmarshal(wire.Data, &failure), "data: %q", wire.Data)
	assert.Equal(t, class, failure.Class)
}

// requireUnreachableTool asserts exit 3's answer for one tool against a
// server whose address nothing listens on.
func requireUnreachableTool(t *testing.T, name string, args map[string]any) {
	t.Helper()
	session := connectMCP(t, unusedAddr(t))
	res, err := callTool(t, session, name, args)
	requireRPCError(t, res, err, codeWorkspaceUnreachable, classUnreachable)
	assert.Contains(t, err.Error(), "workspace unreachable")
}

// TestWorkspaceMCP_OffersExactlyTheFourOperations lists the tools a
// client sees: one per CLI operation and none beyond them (ADR 0028 §4).
func TestWorkspaceMCP_OffersExactlyTheFourOperations(t *testing.T) {
	session := connectMCP(t, unusedAddr(t))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tools, err := session.ListTools(ctx, nil)
	require.NoError(t, err)
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	assert.Equal(t, []string{"exec", "list", "read", "write"}, names)
}

// TestWorkspaceMCP_Read returns a text file as text and other bytes as
// an embedded blob, answers a path outside the workspace with a tool
// error, and an unreachable workspace with JSON-RPC error -32001.
func TestWorkspaceMCP_Read(t *testing.T) {
	addr, dir := startWorkspace(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello\n"), 0o644))
	binary := []byte{0x00, 0xff, 0xfe}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blob.bin"), binary, 0o644))
	session := connectMCP(t, addr)

	res, err := callTool(t, session, "read", map[string]any{"path": "note.txt"})
	assert.Equal(t, "hello\n", resultText(requireToolResult(t, res, err)))

	res, err = callTool(t, session, "read", map[string]any{"path": "blob.bin"})
	requireToolResult(t, res, err)
	require.Len(t, res.Content, 1)
	resource, ok := res.Content[0].(*mcp.EmbeddedResource)
	require.True(t, ok, "non-UTF-8 bytes are an embedded resource, got %T", res.Content[0])
	assert.Equal(t, binary, resource.Resource.Blob)

	res, err = callTool(t, session, "read", map[string]any{"path": "../note.txt"})
	requireWorkspaceRefusal(t, res, err, "InvalidArgument")

	requireUnreachableTool(t, "read", map[string]any{"path": "note.txt"})
}

// TestWorkspaceMCP_Write writes content into the workspace, a path
// that looks like a flag included, answers a path outside it with a
// tool error and writes nothing there, and an unreachable workspace
// with -32001.
func TestWorkspaceMCP_Write(t *testing.T) {
	addr, dir := startWorkspace(t)
	session := connectMCP(t, addr)

	res, err := callTool(t, session, "write", map[string]any{"path": "sub/out.txt", "content": "line one\n"})
	assert.JSONEq(t, `{"bytes_written":9}`, structuredJSON(t, requireToolResult(t, res, err)))
	written, err := os.ReadFile(filepath.Join(dir, "sub", "out.txt"))
	require.NoError(t, err)
	assert.Equal(t, "line one\n", string(written))

	res, err = callTool(t, session, "write", map[string]any{"path": "-dash.txt", "content": "d"})
	requireToolResult(t, res, err)
	assert.FileExists(t, filepath.Join(dir, "-dash.txt"), "a path that looks like a flag is still the path")

	res, err = callTool(t, session, "write", map[string]any{"path": "../escape.txt", "content": "x"})
	requireWorkspaceRefusal(t, res, err, "InvalidArgument")
	assert.NoFileExists(t, filepath.Join(filepath.Dir(dir), "escape.txt"))

	requireUnreachableTool(t, "write", map[string]any{"path": "out.txt", "content": "x"})
}

// TestWorkspaceMCP_List lists a directory as structured entries,
// answers a path outside the workspace with a tool error, and an
// unreachable workspace with -32001.
func TestWorkspaceMCP_List(t *testing.T) {
	addr, dir := startWorkspace(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))
	session := connectMCP(t, addr)

	res, err := callTool(t, session, "list", map[string]any{"path": "."})
	assert.JSONEq(t, `{"entries":[{"name":"a.txt","dir":false},{"name":"sub","dir":true}]}`,
		structuredJSON(t, requireToolResult(t, res, err)))

	res, err = callTool(t, session, "list", map[string]any{"path": ".."})
	requireWorkspaceRefusal(t, res, err, "InvalidArgument")

	requireUnreachableTool(t, "list", map[string]any{"path": "."})
}

// execTool calls exec and decodes its structured result, skipping on a
// host that refuses the isolation every exec asks for, as runExec does.
func execTool(t *testing.T, session *mcp.ClientSession, args map[string]any) execResult {
	t.Helper()
	res, err := callTool(t, session, "exec", args)
	if err == nil && res.IsError && strings.Contains(resultText(res), shell.ErrAgentUIDUnavailable.Error()) {
		if os.Getenv("SHERLOCK_REQUIRE_USERNS") != "" {
			t.Fatalf("SHERLOCK_REQUIRE_USERNS is set but this host refuses the isolated exec: %s", resultText(res))
		}
		t.Skipf("this host cannot provide the isolation every exec asks for: %s", resultText(res))
	}
	requireToolResult(t, res, err)
	var got execResult
	require.NoError(t, json.Unmarshal([]byte(structuredJSON(t, res)), &got))
	return got
}

// TestWorkspaceMCP_Exec runs a command in the workspace directory
// through the shell and as an argv, sends stdin only when given, keeps
// the command's exit code in the result, answers an executable the
// workspace cannot start with a tool error, a timeout the CLI refuses
// with -32603, and an unreachable workspace with -32001.
func TestWorkspaceMCP_Exec(t *testing.T) {
	addr, dir := startWorkspace(t)
	session := connectMCP(t, addr)

	got := execTool(t, session, map[string]any{"command": "pwd; echo oops >&2; exit 3"})
	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	assert.Equal(t, execResult{Stdout: resolved + "\n", Stderr: "oops\n", ExitCode: 3}, got,
		"the command runs in the workspace directory, and its exit code is a result")

	got = execTool(t, session, map[string]any{"command": "printf", "args": []string{"%s|", "a b", "c"}})
	assert.Equal(t, "a b|c|", got.Stdout, "args run as an argv, without a shell")

	got = execTool(t, session, map[string]any{"command": "cat", "stdin": "piped"})
	assert.Equal(t, "piped", got.Stdout, "stdin reaches the command")
	got = execTool(t, session, map[string]any{"command": "cat"})
	assert.Empty(t, got.Stdout, "without stdin nothing is sent")

	res, err := callTool(t, session, "exec", map[string]any{"command": "/nonexistent/program", "args": []string{"arg"}})
	refusal := requireWorkspaceRefusal(t, res, err, "Internal")
	assert.Contains(t, refusal.Message, "/nonexistent/program")

	res, err = callTool(t, session, "exec", map[string]any{"command": "true", "timeout": "soon"})
	requireRPCError(t, res, err, jsonrpc.CodeInternalError, classUsage)

	requireUnreachableTool(t, "exec", map[string]any{"command": "true"})
}
