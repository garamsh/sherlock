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
	"github.com/garamsh/gagent/internal/shell"
	"github.com/garamsh/gagent/internal/tool"
)

// e2eEnvID is this scenario's own environment ID.
const e2eEnvID = "e2e"

// e2eQuestion is the inbound message that drives the canned MVP scenario:
// the mock model stores the question in long-term memory, runs the answer
// through shell_exec, replies with the answer via message_send, and ends
// the turn. After ADR 0012 the canned script exercises the four built-in
// tools (memory, shell_exec, message_send) — the shell_exec call does the
// "compute the answer" step the typed tools cannot.
const e2eQuestion = "what is 6 * 7?"

// cannedShellExecutor is a fake shell.Executor for the e2e scenario. The
// canned script asks for `echo 42`; we return that canned output so the
// test exercises the full tool wiring without requiring root for the real
// fork+exec path.
type cannedShellExecutor struct{}

func (cannedShellExecutor) Run(_ context.Context, _ shell.Request) (shell.Result, error) {
	return shell.Result{Stdout: "42\n", ExitCode: 0}, nil
}

// mvpScript drives the canned MVP scenario through the scripted mock model:
// store the question in memory, compute the answer via shell_exec, reply to
// the sender, then close the turn with a writeback block.
//
// After ADR 0012 the canned script exercises the four built-in tools; the
// shell_exec call is intercepted by the canned executor so the test does
// not require root for the real fork+exec path.
func mvpScript() *agent.ScriptedModel {
	return agent.NewScriptedModel(
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{
			Name: "memory",
			Input: json.RawMessage(
				`{"op":"store","content":"user asked: 6 * 7","type":"episodic","importance_score":0.5,"source":"mock"}`,
			),
		}}},
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
		agent.ScriptedStep{Content: "Replied via message_send and stored the result."},
	)
}

// TestEndToEnd_MVPScenario drives the canned MVP scenario across the real
// wired system in-process: HTTP ingress, memory store, shell_exec (via the
// canned executor), and reply through the file outbox sink. No Docker
// daemon and no real fork+exec required.
func TestEndToEnd_MVPScenario(t *testing.T) {
	root := t.TempDir()
	envID := e2eEnvID

	factory := &gateway.Factory{
		Root: root,
		NewModel: func(string) agent.Model {
			return mvpScript()
		},
		NewShellExecutor: func() shell.Executor {
			return cannedShellExecutor{}
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := gateway.NewRouter(factory, logger)
	t.Cleanup(func() { router.Close() })
	srv := httptest.NewServer(gateway.NewHandler(router, logger))
	t.Cleanup(srv.Close)

	runMVPScenario(t, srv.URL, root, envID)

	// Close drains the router and persists the environment store.
	require.NoError(t, router.Close())
	assertMemoryStore(t, filepath.Join(root, envID))
}

// runMVPScenario drives the canned MVP message flow against a serving
// gateway: HTTP ingress, the full turn lifecycle on the monitoring stream,
// and the reply in the outbox.
func runMVPScenario(t *testing.T, baseURL, root, envID string) {
	t.Helper()

	// Subscribe to the monitoring stream before the message so no event
	// is missed.
	events := subscribeSSE(t, baseURL+"/envs/"+envID+"/events")

	resp := postJSON(t, baseURL+"/envs/"+envID+"/messages",
		`{"content":`+strconvQuote(e2eQuestion)+`}`)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	resp.Body.Close()

	// Monitoring: the full turn lifecycle was published, in order: one
	// turn with three tool calls (memory, shell_exec, message_send) and
	// an empty-ops writeback between every model response.
	got := collectEvents(t, events, 9)
	var kinds []string
	for _, e := range got {
		kinds = append(kinds, e["kind"].(string))
	}
	assert.Equal(t, []string{
		"turn_started",
		"writeback", "tool_call",
		"writeback", "tool_call",
		"writeback", "tool_call",
		"writeback", "turn_finished",
	}, kinds)
	assert.Equal(t, e2eQuestion, got[0]["task"])

	// The agent stored the question, computed via shell_exec, then
	// replied via message_send. The empty final writeback satisfies the
	// contract when no new memories are added this turn.
	assert.Equal(t, "memory", got[2]["tool_name"])
	assert.NotContains(t, got[2], "tool_failed")
	assert.Equal(t, "shell_exec", got[4]["tool_name"])
	assert.NotContains(t, got[4], "tool_failed")
	assert.Equal(t, "message_send", got[6]["tool_name"])
	assert.NotContains(t, got[6], "tool_failed")

	// Reply: message_send output landed in the environment outbox.
	require.Eventually(t, func() bool {
		return len(outboxMessages(t, root, envID)) == 1
	}, 5*time.Second, 10*time.Millisecond)
	replies := outboxMessages(t, root, envID)
	require.Len(t, replies, 1)
	assert.Equal(t, "42", replies[0].Content)
	assert.Equal(t, "http", replies[0].Channel)
	assert.Equal(t, "sender", replies[0].Target)
}

// assertMemoryStore opens the environment's memory store and confirms the
// memory tool call landed an episodic memory containing the recorded
// question.
func assertMemoryStore(t *testing.T, envDir string) {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(envDir, gateway.MemoryDBFile))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	svc := memory.NewService(store, memory.NewHashingEmbedder(), memory.NewConcatSummarizer())
	found, err := tool.Invoke(context.Background(), memory.NewTool(svc), json.RawMessage(
		`{"op":"search","query":"user asked 6 * 7","limit":10}`))
	require.NoError(t, err)
	var hits struct {
		Results []struct {
			ID      int64  `json:"id"`
			Content string `json:"content"`
			Type    string `json:"type"`
		} `json:"results"`
	}
	require.NoError(t, json.Unmarshal(found, &hits))

	var recorded bool
	for _, h := range hits.Results {
		if strings.Contains(h.Content, "user asked: 6 * 7") {
			recorded = true
			assert.Equal(t, "episodic", h.Type,
				"the canned scenario stores the question as an episodic memory")
		}
	}
	assert.True(t, recorded,
		"memory tool call must have stored an episodic memory of the question, got: %s", found)
}
