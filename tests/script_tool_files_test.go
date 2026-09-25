package tests

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/agent/anthropic"
	"github.com/garamsh/sherlock/internal/agent/openai"
	"github.com/garamsh/sherlock/internal/tool"
	"github.com/garamsh/sherlock/internal/workspace"
	"github.com/garamsh/sherlock/internal/workspace/files"
	"github.com/garamsh/sherlock/internal/workspace/server"
	"github.com/garamsh/sherlock/internal/workspace/shell"
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
// the script tool's SHERLOCK_WORKSPACE_ADDR must point at.
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

func installFileTools(t *testing.T) *tool.Registry {
	t.Helper()
	root := t.TempDir()
	pins := tool.ManifestPins{}
	for _, name := range []string{"file_read", "file_write", "file_list"} {
		pins[name] = installTool(t, root, name)
	}
	reg, err := tool.NewRegistry(tool.RegistryOptions{Dir: root, Pins: pins, Required: []string{}})
	require.NoError(t, err)
	return reg
}

func TestScriptTool_Files_Integration(t *testing.T) {
	reg := installFileTools(t)
	addr, fs, _, stop := startFakeWorkspace(t)
	defer stop()
	t.Setenv("SHERLOCK_WORKSPACE_ADDR", addr)
	call := func(name, input, expected string) {
		t.Helper()
		out, err := reg.Call(t.Context(), name, json.RawMessage(input))
		require.NoError(t, err)
		assert.JSONEq(t, expected, string(out))
	}
	call("file_list", `{"path":"."}`, `{"entries":[]}`)
	call("file_write", `{"path":"notes/new.txt","content":"hello 한글\n"}`, `{"bytes_written":13}`)
	body, err := fs.Read("notes/new.txt")
	require.NoError(t, err)
	assert.Equal(t, "hello 한글\n", string(body))
	call("file_read", `{"path":"notes/new.txt"}`, `{"content":"hello 한글\n"}`)
	call("file_list", `{"path":"."}`, `{"entries":[{"name":"notes","dir":true}]}`)
	call("file_list", `{"path":"notes"}`, `{"entries":[{"name":"new.txt","dir":false}]}`)
	call("file_write", `{"path":"notes/new.txt","content":"replacement"}`, `{"bytes_written":11}`)
	call("file_read", `{"path":"notes/new.txt"}`, `{"content":"replacement"}`)
	call("file_write", `{"path":"empty.txt","content":""}`, `{"bytes_written":0}`)
	call("file_read", `{"path":"empty.txt"}`, `{"content":""}`)
	call("file_write", `{"path":"notes/new.txt","content":""}`, `{"bytes_written":0}`)
	call("file_read", `{"path":"notes/new.txt"}`, `{"content":""}`)
	_, err = reg.Call(t.Context(), "file_read", json.RawMessage(`{"path":"missing"}`))
	require.ErrorIs(t, err, tool.ErrExecution)
}

func TestScriptTool_Files_InvalidInputPreservesContent(t *testing.T) {
	reg := installFileTools(t)
	addr, fs, _, stop := startFakeWorkspace(t)
	defer stop()
	t.Setenv("SHERLOCK_WORKSPACE_ADDR", addr)
	_, err := reg.Call(t.Context(), "file_write", json.RawMessage(`{"path":"held.txt","content":"keep"}`))
	require.NoError(t, err)
	for _, input := range []string{
		`{"path":"held.txt"}`, `{"path":"held.txt","content":null}`,
		`{"path":"held.txt","content":42}`, `{"path":"held.txt","content":false}`,
		`{"path":"held.txt","content":{}}`, `{"content":"bad"}`,
		`{"path":"","content":"bad"}`, `{"path":"held.txt","content":"bad","operation":"write"}`,
	} {
		t.Run(input, func(t *testing.T) {
			_, err := reg.Call(t.Context(), "file_write", json.RawMessage(input))
			require.ErrorIs(t, err, tool.ErrValidation)
			body, err := fs.Read("held.txt")
			require.NoError(t, err)
			assert.Equal(t, "keep", string(body))
		})
	}
	for _, name := range []string{"file_read", "file_list"} {
		_, err := reg.Call(t.Context(), name, json.RawMessage(`{"path":"." ,"content":null}`))
		require.ErrorIs(t, err, tool.ErrValidation)
	}
	_, err = reg.Call(t.Context(), "file_write", json.RawMessage(`{"path":"../escape","content":"bad"}`))
	require.ErrorIs(t, err, tool.ErrExecution)
	entries, err := fs.List(".")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "held.txt", entries[0].Name)
}

func TestScriptTool_Files_StalledWorkspaceRefused(t *testing.T) {
	reg := installFileTools(t)
	answering, fs, _, stop := startFakeWorkspace(t)
	defer stop()
	require.NoError(t, fs.Write("hello.txt", []byte("hello")))
	stalledAddr, stalled, stopStalled := startStalledWorkspace(t)
	defer stopStalled()
	for _, tc := range []struct{ name, input string }{
		{"file_read", `{"path":"hello.txt"}`},
		{"file_write", `{"path":"hello.txt","content":"hello"}`},
		{"file_list", `{"path":"."}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SHERLOCK_WORKSPACE_ADDR", answering)
			_, err := reg.Call(t.Context(), tc.name, json.RawMessage(tc.input))
			require.NoError(t, err)
			t.Setenv("SHERLOCK_WORKSPACE_ADDR", stalledAddr)
			start := time.Now()
			_, err = reg.Call(t.Context(), tc.name, json.RawMessage(tc.input))
			elapsed := time.Since(start)
			require.ErrorIs(t, err, tool.ErrExecution)
			assert.GreaterOrEqual(t, elapsed, 10*time.Second)
			assert.Less(t, elapsed, 20*time.Second)
			assert.InDelta(t, 10, stalled.observedDeadline().Seconds(), 2)
		})
	}
}

func TestScriptTool_Files_ProviderExposure(t *testing.T) {
	reg := installFileTools(t)
	addr, fs, _, stop := startFakeWorkspace(t)
	defer stop()
	t.Setenv("SHERLOCK_WORKSPACE_ADDR", addr)
	for _, protocol := range []string{"openai", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			requests := make(chan map[string]json.RawMessage, 1)
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					http.Error(w, err.Error(), 400)
					return
				}
				requests <- request
				w.Header().Set("Content-Type", "application/json")
				if protocol == "openai" {
					_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"write","type":"function","function":{"name":"file_write","arguments":"{\"path\":\"provider.txt\",\"content\":\"wire bytes\"}"}}]}}]}`)
				} else {
					_, _ = io.WriteString(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"write","name":"file_write","input":{"path":"provider.txt","content":"wire bytes"}}]}`)
				}
			}))
			defer endpoint.Close()
			var model agent.Model
			if protocol == "openai" {
				client, err := openai.New(openai.Config{BaseURL: endpoint.URL, Model: "test", APIKey: "test"})
				require.NoError(t, err)
				model = client
			} else {
				client, err := anthropic.New(anthropic.Config{BaseURL: endpoint.URL, Model: "test", APIKey: "test"})
				require.NoError(t, err)
				model = client
			}
			response, err := model.Chat(t.Context(), []agent.Message{{Role: agent.RoleUser, Content: "Write a file."}}, reg.List())
			require.NoError(t, err)
			request := <-requests
			var exposed []struct {
				Name        string          `json:"name"`
				InputSchema json.RawMessage `json:"input_schema"`
				Function    struct {
					Name       string
					Parameters json.RawMessage
					Strict     bool
				} `json:"function"`
			}
			require.NoError(t, json.Unmarshal(request["tools"], &exposed))
			require.Len(t, exposed, 3)
			for _, descriptor := range exposed {
				name, raw := descriptor.Name, descriptor.InputSchema
				if protocol == "openai" {
					name, raw = descriptor.Function.Name, descriptor.Function.Parameters
					assert.True(t, descriptor.Function.Strict)
				}
				var schema struct {
					Properties map[string]json.RawMessage
					Required   []string
				}
				require.NoError(t, json.Unmarshal(raw, &schema))
				expected := []string{"path"}
				if name == "file_write" {
					expected = append(expected, "content")
					assert.JSONEq(t, `{"type":"string"}`, string(schema.Properties["content"]))
				}
				assert.ElementsMatch(t, expected, schema.Required)
				assert.Len(t, schema.Properties, len(expected))
				assert.Contains(t, []string{"file_read", "file_write", "file_list"}, name)
			}
			require.Len(t, response.ToolCalls, 1)
			call := response.ToolCalls[0]
			out, err := reg.Call(t.Context(), call.Name, call.Input)
			require.NoError(t, err)
			assert.JSONEq(t, `{"bytes_written":10}`, string(out))
			body, err := fs.Read("provider.txt")
			require.NoError(t, err)
			assert.Equal(t, "wire bytes", string(body))
		})
	}
}

func TestScriptTool_FileWrite_StdinPreservesContent(t *testing.T) {
	binary := buildToolBinary(t, t.TempDir(), "file_write")
	addr, fs, _, stop := startFakeWorkspace(t)
	defer stop()
	t.Setenv("SHERLOCK_WORKSPACE_ADDR", addr)
	invoke := func(input string) error {
		t.Helper()
		command := exec.Command(binary)
		command.Stdin = strings.NewReader(input)
		return command.Run()
	}
	require.NoError(t, invoke(`{"path":"held.txt","content":"keep"}`))
	for _, input := range []string{
		`{"path":"held.txt"}`, `{"path":"held.txt","content":null}`,
		`{"path":"held.txt","content":42}`, `{"path":"held.txt","content":"bad","extra":true}`,
		`{"path":"held.txt","content":"bad"} {}`, `null`, `{`, ``,
	} {
		t.Run(input, func(t *testing.T) {
			require.Error(t, invoke(input))
			body, err := fs.Read("held.txt")
			require.NoError(t, err)
			assert.Equal(t, "keep", string(body))
		})
	}
	require.NoError(t, invoke(`{"path":"held.txt","content":""}`))
	body, err := fs.Read("held.txt")
	require.NoError(t, err)
	assert.Empty(t, body)
}
