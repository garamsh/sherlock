package files_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/files"
	"github.com/garamsh/gagent/internal/tool"
)

func newFilesTool(t *testing.T) *files.Tool {
	t.Helper()
	fs, err := files.Open(t.TempDir())
	require.NoError(t, err)
	return files.NewTool(fs)
}

func TestFilesTool_InvalidInputRejectedAtBoundary(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "missing operation", input: `{"path":"a.txt"}`},
		{name: "missing path", input: `{"operation":"read"}`},
		{name: "unknown operation", input: `{"operation":"delete","path":"a.txt"}`},
		{name: "write without content", input: `{"operation":"write","path":"a.txt"}`},
		{name: "additional field", input: `{"operation":"read","path":"a.txt","mode":"fast"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filesTool := newFilesTool(t)

			_, err := tool.Invoke(context.Background(), filesTool, json.RawMessage(tt.input))

			require.Error(t, err)
			assert.ErrorIs(t, err, tool.ErrValidation)
		})
	}
}

func TestFilesTool_WriteThenRead(t *testing.T) {
	filesTool := newFilesTool(t)

	out, err := tool.Invoke(context.Background(), filesTool,
		json.RawMessage(`{"operation":"write","path":"notes/a.txt","content":"hello"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"bytes_written":5}`, string(out))

	out, err = tool.Invoke(context.Background(), filesTool,
		json.RawMessage(`{"operation":"read","path":"notes/a.txt"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"content":"hello"}`, string(out))
}

func TestFilesTool_List(t *testing.T) {
	filesTool := newFilesTool(t)
	_, err := tool.Invoke(context.Background(), filesTool,
		json.RawMessage(`{"operation":"write","path":"dir/a.txt","content":"x"}`))
	require.NoError(t, err)

	out, err := tool.Invoke(context.Background(), filesTool,
		json.RawMessage(`{"operation":"list","path":"."}`))

	require.NoError(t, err)
	assert.JSONEq(t, `{"entries":[{"name":"dir","dir":true}]}`, string(out))
}

func TestFilesTool_TraversalIsExecutionError(t *testing.T) {
	filesTool := newFilesTool(t)

	_, err := tool.Invoke(context.Background(), filesTool,
		json.RawMessage(`{"operation":"read","path":"../secret.txt"}`))

	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrExecution)
	assert.ErrorIs(t, err, files.ErrOutsideWorkspace)
}
