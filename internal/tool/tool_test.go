package tool_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/tool"
)

// stubTool is a minimal Tool implementation for exercising the contract.
type stubTool struct {
	name        string
	description string
	inSchema    json.RawMessage
	outSchema   json.RawMessage
	called      bool
	callErr     error
}

func (s *stubTool) Name() string                 { return s.name }
func (s *stubTool) Description() string          { return s.description }
func (s *stubTool) InputSchema() json.RawMessage { return s.inSchema }
func (s *stubTool) OutputSchema() json.RawMessage {
	return s.outSchema
}
func (s *stubTool) Call(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	s.called = true
	if s.callErr != nil {
		return nil, s.callErr
	}
	return json.RawMessage(`{"echo":` + string(input) + `}`), nil
}

const objectSchema = `{
	"type": "object",
	"properties": {"command": {"type": "string"}},
	"required": ["command"],
	"additionalProperties": false
}`

func TestInvoke_ValidInput(t *testing.T) {
	stub := &stubTool{
		name:      "exec",
		inSchema:  json.RawMessage(objectSchema),
		outSchema: json.RawMessage(`{"type": "object"}`),
	}

	out, err := tool.Invoke(context.Background(), stub, json.RawMessage(`{"command":"ls"}`))

	require.NoError(t, err)
	assert.True(t, stub.called)
	assert.JSONEq(t, `{"echo":{"command":"ls"}}`, string(out))
}

func TestInvoke_InvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "missing required field", input: `{}`},
		{name: "wrong field type", input: `{"command":42}`},
		{name: "additional field", input: `{"command":"ls","extra":true}`},
		{name: "malformed JSON", input: `{"command":`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubTool{name: "exec", inSchema: json.RawMessage(objectSchema)}

			_, err := tool.Invoke(context.Background(), stub, json.RawMessage(tt.input))

			require.Error(t, err)
			assert.ErrorIs(t, err, tool.ErrValidation)
			assert.NotErrorIs(t, err, tool.ErrExecution)
			var validationErr *tool.ValidationError
			assert.ErrorAs(t, err, &validationErr)
			assert.False(t, stub.called, "tool body must not run on invalid input")
		})
	}
}

func TestInvoke_ValidationErrorRestatesToolDescription(t *testing.T) {
	stub := &stubTool{
		name:        "exec",
		description: "run a shell command on the workspace",
		inSchema:    json.RawMessage(objectSchema),
	}

	_, err := tool.Invoke(context.Background(), stub, json.RawMessage(`{}`))

	require.ErrorIs(t, err, tool.ErrValidation)
	assert.ErrorContains(t, err, "run a shell command on the workspace",
		"the rejection restates the tool's contract so the model can correct course (issue #160)")
}

func TestInvoke_ValidationErrorOmitsEmptyDescription(t *testing.T) {
	stub := &stubTool{name: "exec", inSchema: json.RawMessage(objectSchema)}

	_, err := tool.Invoke(context.Background(), stub, json.RawMessage(`{}`))

	require.ErrorIs(t, err, tool.ErrValidation)
	assert.NotContains(t, err.Error(), "tool description",
		"a tool without a description keeps the bare validation error")
}

func TestInvoke_ToolError(t *testing.T) {
	stub := &stubTool{
		name:     "exec",
		inSchema: json.RawMessage(objectSchema),
		callErr:  errors.New("command exited 1"),
	}

	_, err := tool.Invoke(context.Background(), stub, json.RawMessage(`{"command":"ls"}`))

	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrExecution)
	assert.NotErrorIs(t, err, tool.ErrValidation)
	assert.ErrorContains(t, err, "command exited 1")
}

func TestInvoke_EmptySchema(t *testing.T) {
	stub := &stubTool{name: "noop"}

	out, err := tool.Invoke(context.Background(), stub, json.RawMessage(`{"anything":true}`))

	require.NoError(t, err)
	assert.JSONEq(t, `{"echo":{"anything":true}}`, string(out))
}
