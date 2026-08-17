package tests

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
	"github.com/garamsh/gagent/internal/message"
	"github.com/garamsh/gagent/internal/tool"
)

// e2eAgentID is this scenario's own agent ID.
const e2eAgentID = "e2e"

// e2eQuestion is the inbound message that drives the canned MVP
// scenario: the mock model sends the canned reply through message_send
// and ends the turn with a memory write recording the answer
// (ADR 0011: the writeback is the single write path).
const e2eQuestion = "what is 6 * 7?"

// mvpScript drives the canned MVP scenario through the scripted mock
// model: reply to the sender through message_send, then close the
// turn with a memory write recording the answer. It is narrower than
// cmd/gagent's shipped mock, which also calls shell_exec and files:
// this harness stubs message_send alone, so the scenario stays free of
// a real workspace, its gRPC listener and a fork+exec. That is the
// harness's scope, not a statement about which tools exist.
func mvpScript() *agent.ScriptedModel {
	return agent.NewScriptedModel(
		resolveStep(),
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{
			Name: "message_send",
			Input: json.RawMessage(
				`{"channel":"http","target":"sender","content":"42"}`,
			),
		}}},
		writebackStep(`{"reply":"Replied via message_send and stored the result.",`+
			`"memory_ops":[{"address":{"subject":"user","question":"what is 6 * 7",`+
			`"restriction":null},"value":"42","operation":null}]}`),
	)
}

// TestEndToEnd_MVPScenario drives the canned MVP scenario across the
// real wired system in-process: HTTP ingress, memory store, the
// message_send tool client (the canned scenario does not exercise
// other tools), and the reply written into the file outbox. No Docker
// daemon, no real gRPC, no fork+exec required.
func TestEndToEnd_MVPScenario(t *testing.T) {
	root := t.TempDir()
	agentID := e2eAgentID

	outbox := newOutboxWriter(root)
	factory := &gateway.Factory{
		MemoryPath: filepath.Join(root, "memory.db"),
		NewModel: func(string) agent.Model {
			return mvpScript()
		},
		Tools: stubClient_{descriptors: []tool.Descriptor{
			{Name: "message_send", Description: "queue an outbound message"},
		}, callFn: func(_ context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
			if name != "message_send" {
				return nil, errors.New("unknown tool " + name)
			}
			var msg message.Message
			if err := json.Unmarshal(input, &msg); err != nil {
				return nil, err
			}
			// The real tool stamps the sender from its environment
			// rather than taking it from the call (ADR 0016,
			// tools/message_send). The stub does the same so the
			// delivered file has the shape a reader meets.
			msg.AgentID = agentID
			if err := outbox.Send(context.Background(), msg); err != nil {
				return nil, err
			}
			return json.RawMessage(`{"status":"queued"}`), nil
		}},
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

// stubClient_ is a minimal platform-tool-client stand-in for tests
// that don't exercise the protocol.
type stubClient_ struct {
	descriptors []tool.Descriptor
	callFn      func(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error)
}

func (s stubClient_) List() []tool.Descriptor {
	out := make([]tool.Descriptor, len(s.descriptors))
	copy(out, s.descriptors)
	return out
}
func (s stubClient_) Call(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	return s.callFn(ctx, name, input)
}

// runMVPScenario drives the canned MVP message flow against a serving
// agent: HTTP ingress, the full turn lifecycle on the monitoring
// stream, and the reply in the outbox.
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
	// turn with one tool call (message_send) and a writeback after
	// every model response.
	got := collectEvents(t, events, 5)
	var kinds []string
	for _, e := range got {
		kinds = append(kinds, e["kind"].(string))
	}
	// The recall event is back: #630 wired the read step, so every turn
	// resolves the task into an address before the act loop (ADR 0019).
	// The writeback fires once per turn, when the turn ends on the
	// reserved call, rather than once per model response (issue #615).
	assert.Equal(t, []string{
		"turn_started", "recall",
		"tool_call", "writeback", "turn_finished",
	}, kinds)
	assert.Contains(t, got[0]["task"], "] unknown: "+e2eQuestion,
		"the turn input renders the message with its ID and sender (ADR 0015)")

	// The agent replied via message_send. The final writeback records
	// the answer at the address of the question it answers (ADR 0011:
	// the writeback is the single write path; ADR 0018).
	assert.Equal(t, "message_send", got[2]["tool_name"])
	assert.NotContains(t, got[2], "tool_failed")

	// Reply: message_send output landed in the agent's outbox.
	require.Eventually(t, func() bool {
		return len(outboxMessages(t, root)) == 1
	}, 5*time.Second, 10*time.Millisecond)
	replies := outboxMessages(t, root)
	require.Len(t, replies, 1)
	assert.Equal(t, "42", replies[0].Content)
	assert.Equal(t, agentID, replies[0].AgentID,
		"a delivered message names the agent that sent it (ADR 0016)")
	assert.Equal(t, "http", replies[0].Channel)
	assert.Equal(t, "sender", replies[0].Target)
}

// assertMemoryStore opens the agent's memory store and confirms the
// writeback is still readable after shutdown, at the address it was
// written to. With curation-only storage (ADR 0006) nothing is
// auto-stored before the turn, so the turn's one write is the only
// memory.
//
// The address is supplied here rather than found. Asked in words an
// addressed store answers nothing at all (ADR 0018), and the step that
// reads a question into an address is #336 step 3 — so this asserts what
// the store can do, which is answer the address it is given.
func assertMemoryStore(t *testing.T, root string) {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(root, "memory.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	svc := memory.NewService(store)
	held, err := svc.At(context.Background(), memory.Address{
		Subject:  "user",
		Question: "what is 6 * 7",
	})
	require.NoError(t, err)

	require.Len(t, held, 1,
		"the turn's one write is readable at its address after shutdown, got: %v", held)
	assert.Equal(t, "42", held[0].Value)
	assert.Equal(t, "agent", held[0].Source)
}
