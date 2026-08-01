package script_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/tool/script"
)

func TestToolScript_Accessors(t *testing.T) {
	ts := script.New(script.Spec{
		Name:         "echo",
		Description:  "echoes stdin",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"string"}`),
	})

	assert.Equal(t, "echo", ts.Name())
	assert.Equal(t, "echoes stdin", ts.Description())
	assert.JSONEq(t, `{"type":"object"}`, string(ts.InputSchema()))
	assert.JSONEq(t, `{"type":"string"}`, string(ts.OutputSchema()))
}

func TestToolScript_Call_RoundTrip(t *testing.T) {
	ts := script.New(script.Spec{
		Name: "echo",
		Exec: script.ExecSpec{
			Command:      []string{"sh", "-c", "cat"},
			StdoutFormat: "json",
		},
	})

	out, err := ts.Call(context.Background(), json.RawMessage(`{"hello":"world"}`))

	require.NoError(t, err)
	assert.JSONEq(t, `{"hello":"world"}`, string(out))
}

func TestToolScript_Call_RequiresCommand(t *testing.T) {
	ts := script.New(script.Spec{Name: "noop"})

	_, err := ts.Call(context.Background(), json.RawMessage(`{}`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), `tool "noop": exec.command is required`)
}

func TestToolScript_Call_RawStdout(t *testing.T) {
	ts := script.New(script.Spec{
		Name: "raw",
		Exec: script.ExecSpec{
			Command: []string{"sh", "-c", "printf 'not json'"},
		},
	})

	out, err := ts.Call(context.Background(), json.RawMessage(`{}`))

	require.NoError(t, err)
	assert.Equal(t, "not json", string(out))
}

func TestToolScript_Call_InvalidJSONOutput(t *testing.T) {
	ts := script.New(script.Spec{
		Name: "bad",
		Exec: script.ExecSpec{
			Command:      []string{"sh", "-c", "printf 'not json'"},
			StdoutFormat: "json",
		},
	})

	_, err := ts.Call(context.Background(), json.RawMessage(`{}`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), `tool "bad": invalid JSON output`)
}

func TestToolScript_Call_Workdir(t *testing.T) {
	dir := t.TempDir()
	ts := script.New(script.Spec{
		Name: "pwd",
		Exec: script.ExecSpec{
			Command: []string{"sh", "-c", "pwd -P"},
			Workdir: dir,
		},
	})

	out, err := ts.Call(context.Background(), json.RawMessage(`{}`))

	require.NoError(t, err)
	want, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	assert.Equal(t, want, strings.TrimSpace(string(out)))
}

func TestToolScript_Call_Env(t *testing.T) {
	ts := script.New(script.Spec{
		Name: "env",
		Exec: script.ExecSpec{
			Command: []string{"sh", "-c", `printf %s "$TOOL_SCRIPT_TEST_VAR"`},
			Env:     []string{"TOOL_SCRIPT_TEST_VAR=hello-env"},
		},
	})

	out, err := ts.Call(context.Background(), json.RawMessage(`{}`))

	require.NoError(t, err)
	assert.Equal(t, "hello-env", string(out))
}

func TestToolScript_Call_Timeout(t *testing.T) {
	ts := script.New(script.Spec{
		Name: "slow",
		Exec: script.ExecSpec{
			// exec replaces the shell so the kill reaches sleep directly;
			// an orphaned grandchild would hold the output pipes open.
			Command: []string{"sh", "-c", "exec sleep 3"},
			Timeout: 100 * time.Millisecond,
		},
	})

	start := time.Now()
	_, err := ts.Call(context.Background(), json.RawMessage(`{}`))
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Less(t, elapsed, 2*time.Second, "the command must be killed at the timeout, not run to completion")
}

func TestToolScript_Call_StderrInError(t *testing.T) {
	ts := script.New(script.Spec{
		Name: "fail",
		Exec: script.ExecSpec{
			Command: []string{"sh", "-c", "echo oops >&2; exit 1"},
		},
	})

	_, err := ts.Call(context.Background(), json.RawMessage(`{}`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), `tool "fail"`)
	assert.Contains(t, err.Error(), "oops", "stderr is included in the error message")
}
