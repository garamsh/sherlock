package tests

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/gateway"
	"github.com/garamsh/sherlock/internal/message"
	"github.com/garamsh/sherlock/internal/tool"
)

// TestScriptTool_MessageSend_Integration drops the message_send tool
// into a tmpdir, points SHERLOCK_OUTBOX_DIR at a tmpdir, and asserts
// the tool writes the outbound message to the outbox and returns
// the queued status. The delivered file carries the sending agent
// (ADR 0016), which the tool takes from its environment rather than
// from the call — the model never states who it is.
func TestScriptTool_MessageSend_Integration(t *testing.T) {
	root := t.TempDir()
	hash := installTool(t, root, "message_send")

	outbox := t.TempDir()
	t.Setenv("SHERLOCK_OUTBOX_DIR", outbox)
	t.Setenv("SHERLOCK_AGENT_ID", "agent-7")

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
	var msg message.Message
	require.NoError(t, json.Unmarshal(body2, &msg))
	assert.Equal(t, "http", msg.Channel)
	assert.Equal(t, "alice", msg.Target)
	assert.Equal(t, "42", msg.Content)
	assert.Equal(t, "agent-7", msg.AgentID,
		"a reader of the outbox knows which agent sent the file")
	assert.Equal(t, msg.ID+".json", entries[0].Name(), "the entry is named by its id")
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
	t.Setenv("SHERLOCK_OUTBOX_DIR", "")

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
	t.Setenv("SHERLOCK_OUTBOX_DIR", outbox)
	t.Setenv("SHERLOCK_AGENT_ID", "agent-7")

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

// TestScriptTool_MessageSend_EntryRepliesToTheDrainedMessage runs one
// turn through the gateway with the real message_send tool, and checks
// the outbox entry the turn wrote names the message the turn drained in
// in_reply_to: the sender's ID reaches the tool byte for byte through the
// session and the runner, not through anything the model says.
func TestScriptTool_MessageSend_EntryRepliesToTheDrainedMessage(t *testing.T) {
	root := t.TempDir()
	toolsDir := t.TempDir()
	hash := installTool(t, toolsDir, "message_send")
	outbox := gateway.DirFor(filepath.Join(root, "memory.db"))
	t.Setenv("SHERLOCK_OUTBOX_DIR", outbox)
	t.Setenv("SHERLOCK_AGENT_ID", "env-1")

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      toolsDir,
		Pins:     tool.ManifestPins{"message_send": hash},
		Required: []string{"message_send"},
	})
	require.NoError(t, err)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	q, err := gateway.NewQueue(&gateway.Factory{
		MemoryPath: filepath.Join(root, "memory.db"),
		NewModel:   func(string) agent.Model { return summarizing{replyScript("reply-1")} },
		Embedder:   tiedEmbedder{},
		Tools:      reg,
	}, "env-1", logger)
	require.NoError(t, err)
	t.Cleanup(func() { q.Close() })
	srv := httptest.NewServer(gateway.NewHandler(q, gateway.Instance{}, logger))
	t.Cleanup(srv.Close)

	const senderID = "garam:msg/7,a"
	resp := postJSON(t, srv.URL+"/agents/env-1/messages", `{"id":"`+senderID+`","content":"hello"}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	var accepted struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&accepted))
	require.Equal(t, senderID, accepted.ID, "the gateway kept the sender's id")

	var entries []string
	require.Eventually(t, func() bool {
		entries, _ = filepath.Glob(filepath.Join(outbox, "*.json"))
		return len(entries) == 1
	}, 10*time.Second, 10*time.Millisecond, "the turn wrote one outbox entry")
	body, err := os.ReadFile(entries[0])
	require.NoError(t, err)
	var msg message.Message
	require.NoError(t, json.Unmarshal(body, &msg))
	assert.Equal(t, []string{senderID}, msg.InReplyTo)
	assert.Equal(t, "reply-1", msg.Content)
}
