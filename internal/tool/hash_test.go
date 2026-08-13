package tool_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/tool"
)

// TestDescriptorHash_Deterministic pins the hash behaviour: the same
// descriptor hashes to the same value every time.
func TestDescriptorHash_Deterministic(t *testing.T) {
	d := tool.Descriptor{
		Name:         "echo",
		Description:  "echo the input",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	}
	h1, err := tool.DescriptorHashForTest(d)
	require.NoError(t, err)
	h2, err := tool.DescriptorHashForTest(d)
	require.NoError(t, err)
	assert.Equal(t, h1, h2)
}

// TestDescriptorHash_FieldOrderIndependent pins the canonical-JSON rule:
// the hash is independent of the field order the descriptor was
// constructed in. A server that reorders its fields does not drift the
// pin.
func TestDescriptorHash_FieldOrderIndependent(t *testing.T) {
	a := tool.Descriptor{
		Name:         "echo",
		Description:  "echo",
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	}
	b := tool.Descriptor{
		Name:         "echo",
		Description:  "echo",
		InputSchema:  json.RawMessage(`{"properties":{"a":{"type":"string"}},"required":["a"],"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	}
	h1, err := tool.DescriptorHashForTest(a)
	require.NoError(t, err)
	h2, err := tool.DescriptorHashForTest(b)
	require.NoError(t, err)
	assert.Equal(t, h1, h2)
}

// TestDescriptorHash_ChangesOnSchema pins the drift detector: a
// different schema yields a different hash. The pin catches the drift.
func TestDescriptorHash_ChangesOnSchema(t *testing.T) {
	a := tool.Descriptor{
		Name:         "echo",
		Description:  "echo",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	}
	b := a
	b.InputSchema = json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer"}}}`)
	h1, _ := tool.DescriptorHashForTest(a)
	h2, _ := tool.DescriptorHashForTest(b)
	assert.NotEqual(t, h1, h2,
		"a schema change must change the hash (and the pin)")
}
