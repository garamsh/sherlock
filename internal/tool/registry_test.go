package tool_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/tool"
)

func TestRegistry_RegisterAndLookup(t *testing.T) {
	reg := tool.NewRegistry()
	stub := &stubTool{name: "exec"}

	require.NoError(t, reg.Register(stub))

	got, err := reg.Lookup("exec")
	require.NoError(t, err)
	assert.Same(t, stub, got)
}

func TestRegistry_RegisterDuplicate(t *testing.T) {
	reg := tool.NewRegistry()
	require.NoError(t, reg.Register(&stubTool{name: "exec"}))

	err := reg.Register(&stubTool{name: "exec"})

	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrDuplicateTool)
}

func TestRegistry_RegisterInvalid(t *testing.T) {
	tests := []struct {
		name string
		tool tool.Tool
	}{
		{name: "nil tool", tool: nil},
		{name: "empty name", tool: &stubTool{name: ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := tool.NewRegistry()

			err := reg.Register(tt.tool)

			require.Error(t, err)
			assert.ErrorIs(t, err, tool.ErrInvalidTool)
		})
	}
}

func TestRegistry_LookupMissing(t *testing.T) {
	reg := tool.NewRegistry()

	_, err := reg.Lookup("missing")

	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrToolNotFound)
}

func TestRegistry_List(t *testing.T) {
	reg := tool.NewRegistry()
	require.NoError(t, reg.Register(&stubTool{
		name:        "beta",
		description: "runs commands",
		inSchema:    json.RawMessage(`{"type":"object"}`),
		outSchema:   json.RawMessage(`{"type":"string"}`),
	}))
	require.NoError(t, reg.Register(&stubTool{
		name:        "alpha",
		description: "reads files",
		inSchema:    json.RawMessage(`{"type":"string"}`),
		outSchema:   json.RawMessage(`{"type":"object"}`),
	}))

	list := reg.List()

	require.Len(t, list, 2)
	assert.Equal(t, "alpha", list[0].Name, "descriptors are sorted by name")
	assert.Equal(t, "beta", list[1].Name)
	assert.Equal(t, "reads files", list[0].Description, "the descriptor carries the tool's description")
	assert.Equal(t, "runs commands", list[1].Description)
	assert.JSONEq(t, `{"type":"string"}`, string(list[0].InputSchema))
	assert.JSONEq(t, `{"type":"object"}`, string(list[0].OutputSchema))
}

func TestRegistry_ListEmpty(t *testing.T) {
	reg := tool.NewRegistry()

	assert.Empty(t, reg.List())
}
