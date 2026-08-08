package tool_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/tool"
)

func newWireServer(t *testing.T, tools ...tool.Tool) (*tool.Registry, *httptest.Server) {
	t.Helper()
	reg := tool.NewRegistry()
	for _, tt := range tools {
		require.NoError(t, reg.Register(tt))
	}
	srv := httptest.NewServer(tool.NewHandler(reg))
	t.Cleanup(srv.Close)
	return reg, srv
}

func discoverOne(t *testing.T, srv *httptest.Server) tool.Descriptor {
	t.Helper()
	descs, err := tool.Discover(context.Background(), srv.URL)
	require.NoError(t, err)
	require.Len(t, descs, 1)
	return descs[0]
}

func TestDiscover_ListsServedDescriptors(t *testing.T) {
	reg, srv := newWireServer(t,
		&stubTool{
			name:        "beta",
			description: "runs commands",
			inSchema:    json.RawMessage(objectSchema),
			outSchema:   json.RawMessage(`{"type":"object"}`),
		},
		&stubTool{
			name:        "alpha",
			description: "reads files",
			inSchema:    json.RawMessage(`{"type":"string"}`),
			outSchema:   json.RawMessage(`{"type":"object"}`),
		},
	)

	descs, err := tool.Discover(context.Background(), srv.URL)

	require.NoError(t, err)
	served := reg.List()
	require.Len(t, descs, len(served))
	for i, want := range served {
		assert.Equal(t, want.Name, descs[i].Name)
		assert.Equal(t, want.Description, descs[i].Description)
		assert.JSONEq(t, string(want.InputSchema), string(descs[i].InputSchema))
		assert.JSONEq(t, string(want.OutputSchema), string(descs[i].OutputSchema))
	}
}

func TestRemoteTool_BehavesLikeInProcessTool(t *testing.T) {
	stub := &stubTool{
		name:        "exec",
		description: "runs commands",
		inSchema:    json.RawMessage(objectSchema),
		outSchema:   json.RawMessage(`{"type":"object"}`),
	}
	_, srv := newWireServer(t, stub)
	remote := tool.NewRemoteTool(discoverOne(t, srv), srv.URL)

	local := tool.NewRegistry()
	require.NoError(t, local.Register(remote), "a remote tool joins a local registry like any other tool")
	joined, err := local.Lookup("exec")
	require.NoError(t, err)

	input := json.RawMessage(`{"command":"ls"}`)
	got, err := tool.Invoke(context.Background(), joined, input)
	require.NoError(t, err)
	want, err := tool.Invoke(context.Background(), stub, input)
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(got))
}

func TestRemoteTool_ValidationErrorCrossesTheWire(t *testing.T) {
	stub := &stubTool{
		name:        "exec",
		description: "run a shell command on the workspace",
		inSchema:    json.RawMessage(objectSchema),
	}
	_, srv := newWireServer(t, stub)
	remote := tool.NewRemoteTool(discoverOne(t, srv), srv.URL)

	// Call directly, bypassing the client-side Invoke validation, so the
	// server boundary is the one that rejects the input.
	_, err := remote.Call(context.Background(), json.RawMessage(`{}`))

	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrValidation)
	assert.NotErrorIs(t, err, tool.ErrExecution)
	var validationErr *tool.ValidationError
	require.ErrorAs(t, err, &validationErr)
	assert.Equal(t, "exec", validationErr.Tool)
	assert.ErrorContains(t, err, "run a shell command on the workspace",
		"the description survives the wire with the rejection")
	assert.False(t, stub.called, "the tool body must not run on invalid input")
}

func TestRemoteTool_ExecutionErrorCrossesTheWire(t *testing.T) {
	stub := &stubTool{
		name:     "exec",
		inSchema: json.RawMessage(objectSchema),
		callErr:  errors.New("command exited 1"),
	}
	_, srv := newWireServer(t, stub)
	remote := tool.NewRemoteTool(discoverOne(t, srv), srv.URL)

	_, err := remote.Call(context.Background(), json.RawMessage(`{"command":"ls"}`))

	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrExecution)
	assert.NotErrorIs(t, err, tool.ErrValidation)
	var executionErr *tool.ExecutionError
	require.ErrorAs(t, err, &executionErr)
	assert.Equal(t, "exec", executionErr.Tool)
	assert.ErrorContains(t, err, "command exited 1")
}

func TestRemoteTool_UnknownTool(t *testing.T) {
	_, srv := newWireServer(t)
	remote := tool.NewRemoteTool(tool.Descriptor{Name: "missing"}, srv.URL)

	_, err := remote.Call(context.Background(), json.RawMessage(`{}`))

	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrToolNotFound)
}
