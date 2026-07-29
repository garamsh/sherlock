package tool_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/tool"
)

const personSchema = `{
	"type": "object",
	"properties": {"name": {"type": "string"}},
	"required": ["name"]
}`

func TestInvoke_EmptySchemaAcceptsAnyInput(t *testing.T) {
	stub := &stubTool{name: "person"}

	_, err := tool.Invoke(context.Background(), stub, json.RawMessage(`{"anything": 1}`))

	require.NoError(t, err)
}

func TestInvoke_SchemaValidInputPasses(t *testing.T) {
	stub := &stubTool{name: "person", inSchema: json.RawMessage(personSchema)}

	_, err := tool.Invoke(context.Background(), stub, json.RawMessage(`{"name": "ada"}`))

	require.NoError(t, err)
}

func TestInvoke_SchemaInvalidInputFails(t *testing.T) {
	stub := &stubTool{name: "person", inSchema: json.RawMessage(personSchema)}

	_, err := tool.Invoke(context.Background(), stub, json.RawMessage(`{"name": 42}`))

	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrValidation)
}

func TestInvoke_ValidationErrorOmitsHostFilesystemPath(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)
	stub := &stubTool{name: "person", inSchema: json.RawMessage(personSchema)}

	_, err = tool.Invoke(context.Background(), stub, json.RawMessage(`{"name": 42}`))

	require.Error(t, err)
	msg := err.Error()
	assert.NotContains(t, msg, "file://", "validation error must not leak a file URL")
	assert.NotContains(t, msg, cwd, "validation error must not leak the working directory")
}
