package tests

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
	"github.com/garamsh/gagent/internal/tool"
)

// e2eAgentID is this scenario's own agent ID.
const e2eAgentID = "e2e"

// e2eQuestion is the inbound message that drives the canned MVP scenario:
// the mock model computes the answer through a discovered shell_exec,
// replies with the answer via message_send, and ends the turn with a
// writeback block recording the question (ADR 0011: the writeback is the
// single write path).
const e2eQuestion = "what is 6 * 7?"

// cannedShellTool is the fake computer tool the in-process scenario
// serves over the contract: the canned script asks for `echo 42` and
// gets the canned output back.
type cannedShellTool struct{}

func (cannedShellTool) Name() string        { return "shell_exec" }
func (cannedShellTool) Description() string { return "Run a shell command." }
func (cannedShellTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`)
}
func (cannedShellTool) OutputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"stdout":{"type":"string"},"stderr":{"type":"string"},"exit_code":{"type":"integer"}}}`)
}

func (cannedShellTool) Call(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"stdout":"42\n","stderr":"","exit_code":0}`), nil
}

// serveComputer starts an in-process tool server (the sidecar's shape)
// serving the canned shell_exec, discovers it the way the agent does at
// startup, and returns the discovered remote tools.
func serveComputer(t *testing.T) []tool.Tool {
	t.Helper()
	registry := tool.NewRegistry()
	require.NoError(t, registry.Register(cannedShellTool{}))
	srv := httptest.NewServer(tool.NewHandler(registry))
	t.Cleanup(srv.Close)

	descs, err := tool.Discover(context.Background(), srv.URL)
	require.NoError(t, err)
	var tools []tool.Tool
	for _, d := range descs {
		tools = append(tools, tool.NewRemoteTool(d, srv.URL))
	}
	return tools
}

// mvpScript drives the canned MVP scenario through the scripted mock
// model: compute the answer via a discovered shell_exec, reply to the
// sender, then close the turn with a writeback block recording the
// question.
func mvpScript() *agent.ScriptedModel {
	return agent.NewScriptedModel(
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{
			Name: "shell_exec",
			Input: json.RawMessage(
				`{"command":"echo 42"}`,
			),
		}}},
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{
			Name: "message_send",
			Input: json.RawMessage(
				`{"channel":"http","target":"sender","content":"42"}`,
			),
		}}},
		agent.ScriptedStep{Content: "Replied via message_send and stored the result.\n" +
			"```memory\n" +
			`{"add":[{"content":"user asked: 6 * 7","type":"episodic"}]}` + "\n```"},
	)
}

// TestEndToEnd_MVPScenario drives the canned MVP scenario across the real
// wired system in-process: HTTP ingress, memory store, a discovered
// computer tool over the tool contract, and the reply through the file
// outbox sink. No Docker daemon and no real fork+exec required.
func TestEndToEnd_MVPScenario(t *testing.T) {
	root := t.TempDir()
	agentID := e2eAgentID

	factory := &gateway.Factory{
		MemoryPath: filepath.Join(root, "memory.db"),
		NewModel: func(string) agent.Model {
			return mvpScript()
		},
		Tools: serveComputer(t),
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	q, err := gateway.NewQueue(factory, agentID, logger)
	require.NoError(t, err)
	t.Cleanup(func() { q.Close() })
	srv := httptest.NewServer(gateway.NewHandler(q, logger))
	t.Cleanup(srv.Close)

	runMVPScenario(t, srv.URL, root, agentID)

	// Close drains the queue and persists the memory store.
	require.NoError(t, q.Close())
	assertMemoryStore(t, root)
}

// runMVPScenario drives the canned MVP message flow against a serving
// agent: HTTP ingress, the full turn lifecycle on the monitoring stream,
// and the reply in the outbox.
func runMVPScenario(t *testing.T, baseURL, root, agentID string) {
	t.Helper()

	// Subscribe to the monitoring stream before the message so no event
	// is missed.
	events := subscribeSSE(t, baseURL+"/agents/"+agentID+"/events")

	resp := postJSON(t, baseURL+"/agents/"+agentID+"/messages",
		`{"content":`+strconvQuote(e2eQuestion)+`}`)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	resp.Body.Close()

	// Monitoring: the full turn lifecycle was published, in order: one
	// turn with two tool calls (a discovered shell_exec, message_send)
	// and a writeback after every model response.
	got := collectEvents(t, events, 7)
	var kinds []string
	for _, e := range got {
		kinds = append(kinds, e["kind"].(string))
	}
	assert.Equal(t, []string{
		"turn_started",
		"writeback", "tool_call",
		"writeback", "tool_call",
		"writeback", "turn_finished",
	}, kinds)
	assert.Contains(t, got[0]["task"], "] unknown: "+e2eQuestion,
		"the turn input renders the message with its ID and sender (ADR 0016)")

	// The agent computed via a discovered shell_exec, then replied via
	// message_send. The final writeback records the question as an
	// episodic memory (ADR 0011: the writeback is the single write path).
	assert.Equal(t, "shell_exec", got[2]["tool_name"])
	assert.NotContains(t, got[2], "tool_failed")
	assert.Equal(t, "message_send", got[4]["tool_name"])
	assert.NotContains(t, got[4], "tool_failed")

	// Reply: message_send output landed in the agent's outbox.
	require.Eventually(t, func() bool {
		return len(outboxMessages(t, root)) == 1
	}, 5*time.Second, 10*time.Millisecond)
	replies := outboxMessages(t, root)
	require.Len(t, replies, 1)
	assert.Equal(t, "42", replies[0].Content)
	assert.Equal(t, "http", replies[0].Channel)
	assert.Equal(t, "sender", replies[0].Target)
}

// assertMemoryStore opens the agent's memory store and confirms the
// recorded question is still queryable after shutdown. ADR 0012 captures
// the inbound message as a second episodic memory, so shutdown
// consolidation (ADR 0011) compresses the captured turn and the writeback
// add into one semantic parent; the assertion matches that parent, which
// carries both contents.
func assertMemoryStore(t *testing.T, root string) {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(root, "memory.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	svc := memory.NewService(store, memory.NewHashingEmbedder(), memory.NewConcatSummarizer())
	found, err := svc.Search(context.Background(), "user asked 6 * 7", 10)
	require.NoError(t, err)

	var recorded bool
	for _, h := range found {
		if strings.Contains(h.Content, "user asked: 6 * 7") {
			recorded = true
			assert.Equal(t, memory.TypeSemantic, h.Type,
				"shutdown consolidation compresses the captured turn and the writeback add into one semantic parent")
			assert.Contains(t, h.Content, e2eQuestion,
				"the auto-captured user turn survives inside the consolidated parent")
		}
	}
	assert.True(t, recorded,
		"the recorded question must stay queryable after shutdown, got: %v", found)
}
