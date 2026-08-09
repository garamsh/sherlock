package shellexec_test

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/garamsh/gagent/internal/tool"
	"github.com/garamsh/gagent/internal/tool/tooltest"
	"github.com/garamsh/gagent/internal/workspace"
	"github.com/garamsh/gagent/internal/workspace/shell"
	shellexec "github.com/garamsh/gagent/tools/shell_exec"
)

// schemaFile mirrors the embed in tools/shell_exec/shell_exec.go so the
// test reads the same shipped bytes the runtime serves.
//
//go:embed schema.json
var schemaFile []byte

func TestEmbeddedSchema_Conforms(t *testing.T) {
	tooltest.CheckSchema(t, "shell_exec", schemaFile)
}

func TestToolSchema_AccessorsReturnEmbeddedBytes(t *testing.T) {
	if len(shellexec.InputSchema()) == 0 {
		t.Fatal("shellexec.InputSchema returned empty bytes")
	}
	if len(shellexec.OutputSchema()) == 0 {
		t.Fatal("shellexec.OutputSchema returned empty bytes")
	}
}

// TestSchema_MatchesStructTags is the build-time schema/struct drift
// guard: the input schema's properties/required must match CallInput's
// json tags, and the output schema must match shell.Result's.
func TestSchema_MatchesStructTags(t *testing.T) {
	input, output := tooltest.SubSchemas(t, schemaFile)
	tooltest.CheckStructShape(t, input, reflect.TypeOf(shellexec.CallInput{}))
	tooltest.CheckStructShape(t, output, reflect.TypeOf(shell.Result{}))
}

// fakeWorkspace is the in-memory workspace client the tool's logic is
// tested against: it records the Exec request and replays a canned
// response. The unit under test is the tool's input-to-request
// translation, not the executor's fork+exec implementation (that lives
// in internal/workspace/shell and is covered there).
type fakeWorkspace struct {
	workspace.WorkspaceClient
	req  *workspace.ExecRequest
	resp *workspace.ExecResponse
	err  error
}

func (f *fakeWorkspace) Exec(_ context.Context, req *workspace.ExecRequest, _ ...grpc.CallOption) (*workspace.ExecResponse, error) {
	f.req = req
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func TestShellExecTool_InvalidInputRejectedAtBoundary(t *testing.T) {
	execTool := shellexec.New(&fakeWorkspace{})

	tests := []struct {
		name  string
		input string
	}{
		{name: "missing command", input: `{"timeout_seconds":5}`},
		{name: "empty command", input: `{"command":""}`},
		{name: "timeout below range", input: `{"command":"true","timeout_seconds":0}`},
		{name: "timeout above range", input: `{"command":"true","timeout_seconds":3601}`},
		{name: "additional field", input: `{"command":"true","workdir":"/tmp"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.Invoke(context.Background(), execTool, json.RawMessage(tt.input))
			require.Error(t, err)
			assert.ErrorIs(t, err, tool.ErrValidation)
		})
	}
}

func TestShellExecTool_CallsWorkspaceExecRPC(t *testing.T) {
	ws := &fakeWorkspace{resp: &workspace.ExecResponse{Stdout: "ok\n"}}
	execTool := shellexec.New(ws)

	out, err := tool.Invoke(context.Background(), execTool,
		json.RawMessage(`{"command":"ls"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"stdout":"ok\n","stderr":"","exit_code":0,"timed_out":false}`, string(out))

	assert.Equal(t, "ls", ws.req.GetCommand())
	assert.True(t, ws.req.GetNoNetwork(),
		"shell_exec children run in a fresh, empty network namespace (ADR 0010)")
	assert.Zero(t, ws.req.GetTimeoutNanos(),
		"no timeout in the input leaves the ceiling to the workspace's exec-timeout")
	assert.Empty(t, ws.req.GetWorkdir(),
		"no workdir crosses the wire: exec children work in the workspace directory")
}

func TestShellExecTool_TimeoutSecondsOverridesWorkspaceDefault(t *testing.T) {
	ws := &fakeWorkspace{resp: &workspace.ExecResponse{}}
	execTool := shellexec.New(ws)

	_, err := tool.Invoke(context.Background(), execTool,
		json.RawMessage(`{"command":"sleep 1","timeout_seconds":5}`))
	require.NoError(t, err)
	assert.Equal(t, int64(5*time.Second), ws.req.GetTimeoutNanos())
}

func TestShellExecTool_TimeoutIsAResultNotAFailure(t *testing.T) {
	ws := &fakeWorkspace{resp: &workspace.ExecResponse{Stdout: "partial", TimedOut: true}}
	execTool := shellexec.New(ws)

	out, err := tool.Invoke(context.Background(), execTool,
		json.RawMessage(`{"command":"sleep 60"}`))
	require.NoError(t, err,
		"a timed-out command is a normal outcome per ADR 0008, not a tool failure")
	assert.JSONEq(t, `{"stdout":"partial","stderr":"","exit_code":0,"timed_out":true}`, string(out))
}

func TestShellExecTool_ExecFailureIsAToolError(t *testing.T) {
	ws := &fakeWorkspace{err: errors.New("shell: agent_uid unavailable")}
	execTool := shellexec.New(ws)

	_, err := tool.Invoke(context.Background(), execTool,
		json.RawMessage(`{"command":"true"}`))
	require.Error(t, err,
		"a workspace-side failure — e.g. the fail-closed privilege drop — is a tool error")
	assert.Contains(t, err.Error(), "agent_uid unavailable")
}
