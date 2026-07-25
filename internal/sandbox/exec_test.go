package sandbox_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/sandbox"
)

// fakeRuntime is a recording Runtime fake.
type fakeRuntime struct {
	created   []sandbox.Config
	execs     []string
	destroyed []string
	createErr error
	execErr   error
}

func (f *fakeRuntime) Create(_ context.Context, cfg sandbox.Config) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}
	f.created = append(f.created, cfg)
	return "container-1", nil
}

func (f *fakeRuntime) Exec(_ context.Context, id string, command []string, _ time.Duration) (sandbox.Result, error) {
	f.execs = append(f.execs, command[len(command)-1])
	if f.execErr != nil {
		return sandbox.Result{}, f.execErr
	}
	return sandbox.Result{Stdout: "ran: " + command[len(command)-1], ExitCode: 0}, nil
}

func (f *fakeRuntime) Destroy(_ context.Context, id string) error {
	f.destroyed = append(f.destroyed, id)
	return nil
}

func testConfig() sandbox.Config {
	return sandbox.Config{EnvID: "env-1", Workspace: "/tmp/ws", Image: "alpine:3.21"}
}

func TestExecTool_CallRunsCommandInContainer(t *testing.T) {
	rt := &fakeRuntime{}
	tool := sandbox.NewExecTool(rt, testConfig(), time.Second)

	out, err := tool.Call(context.Background(), json.RawMessage(`{"command":"ls -la"}`))

	require.NoError(t, err)
	require.Len(t, rt.created, 1)
	assert.Equal(t, testConfig(), rt.created[0])
	assert.Equal(t, []string{"ls -la"}, rt.execs)
	assert.JSONEq(t, `{"stdout":"ran: ls -la","stderr":"","exit_code":0,"timed_out":false}`, string(out))
}

func TestExecTool_ContainerCreatedOnce(t *testing.T) {
	rt := &fakeRuntime{}
	tool := sandbox.NewExecTool(rt, testConfig(), time.Second)

	_, err := tool.Call(context.Background(), json.RawMessage(`{"command":"one"}`))
	require.NoError(t, err)
	_, err = tool.Call(context.Background(), json.RawMessage(`{"command":"two"}`))
	require.NoError(t, err)

	assert.Len(t, rt.created, 1)
	assert.Equal(t, []string{"one", "two"}, rt.execs)
}

func TestExecTool_CreateErrorPropagates(t *testing.T) {
	rt := &fakeRuntime{createErr: errors.New("daemon down")}
	tool := sandbox.NewExecTool(rt, testConfig(), time.Second)

	_, err := tool.Call(context.Background(), json.RawMessage(`{"command":"ls"}`))

	require.Error(t, err)
	assert.ErrorContains(t, err, "daemon down")
	assert.Empty(t, rt.execs)
}

func TestExecTool_ExecErrorPropagates(t *testing.T) {
	rt := &fakeRuntime{execErr: errors.New("container gone")}
	tool := sandbox.NewExecTool(rt, testConfig(), time.Second)

	_, err := tool.Call(context.Background(), json.RawMessage(`{"command":"ls"}`))

	require.Error(t, err)
	assert.ErrorContains(t, err, "container gone")
}

func TestExecTool_DestroyIsIdempotent(t *testing.T) {
	rt := &fakeRuntime{}
	tool := sandbox.NewExecTool(rt, testConfig(), time.Second)

	require.NoError(t, tool.Destroy(context.Background()))
	assert.Empty(t, rt.destroyed, "destroy before create must not touch the runtime")

	_, err := tool.Call(context.Background(), json.RawMessage(`{"command":"ls"}`))
	require.NoError(t, err)
	require.NoError(t, tool.Destroy(context.Background()))
	require.NoError(t, tool.Destroy(context.Background()))

	assert.Equal(t, []string{"container-1"}, rt.destroyed)
}
