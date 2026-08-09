package shellexec_test

import (
	"context"
	_ "embed"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/shell"
	"github.com/garamsh/gagent/internal/tool"
	"github.com/garamsh/gagent/internal/tool/tooltest"
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

// fakeExecutor is the in-memory shell.Executor the tool's logic is
// tested against: it records the request and replays a canned result.
// The unit under test is the tool's input-to-request translation, not
// the executor's fork+exec implementation (that lives in
// internal/shell and is covered there).
type fakeExecutor struct {
	req     shell.Request
	result  shell.Result
	err     error
	timeout time.Duration
}

func (f *fakeExecutor) Run(_ context.Context, req shell.Request) (shell.Result, error) {
	f.req = req
	f.timeout = req.Timeout
	return f.result, f.err
}

func TestShellExecTool_InvalidInputRejectedAtBoundary(t *testing.T) {
	execTool := shellexec.New(&fakeExecutor{}, "/workspace", 30*time.Second)

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

func TestShellExecTool_RunsInSharedWorkspaceDirectory(t *testing.T) {
	exec := &fakeExecutor{result: shell.Result{Stdout: "ok\n", ExitCode: 0}}
	execTool := shellexec.New(exec, "/workspace", 30*time.Second)

	out, err := tool.Invoke(context.Background(), execTool,
		json.RawMessage(`{"command":"ls"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"stdout":"ok\n","stderr":"","exit_code":0,"timed_out":false}`, string(out))

	assert.Equal(t, "ls", exec.req.Command)
	assert.Equal(t, "/workspace", exec.req.Workdir,
		"the child chdirs into the shared workspace directory")
	assert.True(t, exec.req.NoNetwork,
		"shell_exec children run in a fresh, empty network namespace (ADR 0010)")
	assert.Equal(t, 30*time.Second, exec.timeout,
		"the configured default bounds the call when timeout_seconds is absent")
}

func TestShellExecTool_TimeoutSecondsOverridesDefault(t *testing.T) {
	exec := &fakeExecutor{}
	execTool := shellexec.New(exec, "/workspace", 30*time.Second)

	_, err := tool.Invoke(context.Background(), execTool,
		json.RawMessage(`{"command":"sleep 1","timeout_seconds":5}`))
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, exec.timeout)
}

func TestShellExecTool_TimeoutIsAResultNotAFailure(t *testing.T) {
	exec := &fakeExecutor{
		result: shell.Result{Stdout: "partial", TimedOut: true},
		err:    shell.ErrTimedOut,
	}
	execTool := shellexec.New(exec, "/workspace", 30*time.Second)

	out, err := tool.Invoke(context.Background(), execTool,
		json.RawMessage(`{"command":"sleep 60"}`))
	require.NoError(t, err,
		"a timed-out command is a normal outcome per ADR 0008, not a tool failure")
	assert.JSONEq(t, `{"stdout":"partial","stderr":"","exit_code":0,"timed_out":true}`, string(out))
}
