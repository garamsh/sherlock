package tests

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/tool"
)

// TestScriptTool_MessageSend_Integration drops the message_send tool
// into a tmpdir, points GAGENT_OUTBOX_DIR at a tmpdir, and asserts
// the tool writes the outbound message to the outbox and returns
// the queued status. The delivered file carries the sending agent
// (ADR 0016), which the tool takes from its environment rather than
// from the call — the model never states who it is.
func TestScriptTool_MessageSend_Integration(t *testing.T) {
	root := t.TempDir()
	hash := installTool(t, root, "message_send")

	outbox := t.TempDir()
	t.Setenv("GAGENT_OUTBOX_DIR", outbox)
	t.Setenv("GAGENT_AGENT_ID", "agent-7")

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"message_send": hash},
		Required: []string{"message_send"},
	})
	require.NoError(t, err)

	out, err := reg.Call(context.Background(), "message_send",
		json.RawMessage(`{"channel":"http","target":"alice","content":"42"}`))
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(out, &body))
	assert.Equal(t, "queued", body["status"])

	// The tool wrote the message to the outbox directory.
	entries, err := os.ReadDir(outbox)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	body2, err := os.ReadFile(filepath.Join(outbox, entries[0].Name()))
	require.NoError(t, err)
	var msg map[string]string
	require.NoError(t, json.Unmarshal(body2, &msg))
	assert.Equal(t, "http", msg["channel"])
	assert.Equal(t, "alice", msg["target"])
	assert.Equal(t, "42", msg["content"])
	assert.Equal(t, "agent-7", msg["agent_id"],
		"a reader of the outbox knows which agent sent the file")
}

// TestScriptTool_MessageSend_MissingOutboxDir covers the error path
// when the outbox directory is not configured: the script exits
// non-zero and the registry surfaces the failure as an execution
// error.
func TestScriptTool_MessageSend_MissingOutboxDir(t *testing.T) {
	root := t.TempDir()
	hash := installTool(t, root, "message_send")
	// Explicitly clear the env; t.Setenv with empty would set
	// the variable to the empty string, which is the failure
	// we want to test.
	t.Setenv("GAGENT_OUTBOX_DIR", "")

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"message_send": hash},
		Required: []string{"message_send"},
	})
	require.NoError(t, err)

	_, err = reg.Call(context.Background(), "message_send",
		json.RawMessage(`{"channel":"http","target":"alice","content":"42"}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrExecution)
}

// TestScriptTool_MessageSend_InputRejected pins the boundary: a
// missing field is rejected before the script runs.
func TestScriptTool_MessageSend_InputRejected(t *testing.T) {
	root := t.TempDir()
	hash := installTool(t, root, "message_send")
	outbox := t.TempDir()
	t.Setenv("GAGENT_OUTBOX_DIR", outbox)
	t.Setenv("GAGENT_AGENT_ID", "agent-7")

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"message_send": hash},
		Required: []string{"message_send"},
	})
	require.NoError(t, err)

	_, err = reg.Call(context.Background(), "message_send",
		json.RawMessage(`{"channel":"http","target":"alice"}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrValidation)

	// The script never ran, so the outbox is empty.
	entries, err := os.ReadDir(outbox)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	assert.Empty(t, names, "rejected input must not write to the outbox")
}
