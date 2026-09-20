// Package tests holds end-to-end scenarios that wire multiple domains
// together (docs/convention/testing.md).
package tests

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/message"
	"github.com/garamsh/gagent/internal/tool"
)

// gatedModel wraps the scripted mock model: its first Chat call blocks
// until release closes, so the test can hold a run open while a second
// message arrives.
type gatedModel struct {
	inner   *agent.ScriptedModel
	release <-chan struct{}

	once    sync.Once
	started chan struct{}
}

func (m *gatedModel) Chat(ctx context.Context, msgs []agent.Message, tools []tool.Descriptor) (agent.Response, error) {
	var first bool
	m.once.Do(func() { first = true; close(m.started) })
	if first {
		select {
		case <-m.release:
		case <-ctx.Done():
			return agent.Response{}, ctx.Err()
		}
	}
	return m.inner.Chat(ctx, msgs, tools)
}

// replyScript scripts one full turn per message: the read step's call,
// message_send with the given reply, then the reserved writeback the
// turn ends on.
func replyScript(replies ...string) *agent.ScriptedModel {
	var steps []agent.ScriptedStep
	for _, reply := range replies {
		steps = append(steps,
			resolveStep(),
			fixtureCheckpoint("active"),
			agent.ScriptedStep{ToolCalls: []agent.ToolCall{{
				Name: "message_send",
				Input: json.RawMessage(
					`{"channel":"http","target":"sender","content":` + strconvQuote(reply) + `}`),
			}}},
			writebackStep(`{"reply":"Replied.","memory_ops":[{"address":{`+
				`"subject":"the http channel","question":"what did the http channel ask for",`+
				`"restriction":null},"value":"an acknowledgement","operation":null}]}`),
		)
	}
	return agent.NewScriptedModel(steps...)
}

func strconvQuote(s string) string {
	q, _ := json.Marshal(s)
	return string(q)
}

// newGatewayQueue starts the one agent's queue over a real factory: the
// memory file and the file outbox live under root. The factory holds
// a stub tool client that forwards message_send to the same file outbox the
// gateway writes to, so the test exercises the platform protocol
// client surface end-to-end.
func newGatewayQueue(t *testing.T, root string, newModel func(string) agent.Model) *gateway.Queue {
	t.Helper()
	outbox := newOutboxWriter(root)
	factory := &gateway.Factory{
		MemoryPath: filepath.Join(root, "memory.db"),
		NewModel:   newModel,
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
			if err := outbox.Send(context.Background(), msg); err != nil {
				return nil, err
			}
			return json.RawMessage(`{"status":"queued"}`), nil
		}},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	q, err := gateway.NewQueue(factory, "env-1", logger)
	require.NoError(t, err)
	return q
}

func TestGateway_EndToEnd(t *testing.T) {
	root := t.TempDir()
	release := make(chan struct{})
	gated := &gatedModel{inner: replyScript("reply-1", "reply-2"), release: release, started: make(chan struct{})}

	q := newGatewayQueue(t, root, func(string) agent.Model { return gated })
	t.Cleanup(func() { q.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(gateway.NewHandler(q, logger))
	t.Cleanup(srv.Close)

	// Subscribe to the monitoring stream before any message so no event
	// is missed.
	events := subscribeSSE(t, srv.URL+"/agents/env-1/events")

	// First message: accepted and its run starts (blocking in the model).
	resp := postJSON(t, srv.URL+"/agents/env-1/messages", `{"content":"first"}`)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	resp.Body.Close()
	select {
	case <-gated.started:
	case <-time.After(5 * time.Second):
		t.Fatal("first run did not start")
	}

	// Second message mid-run: queued as a followup, not run yet.
	resp = postJSON(t, srv.URL+"/agents/env-1/messages", `{"content":"second"}`)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	resp.Body.Close()
	assert.Empty(t, outboxMessages(t, root))

	close(release)

	// Both runs complete and deliver their replies to the outbox, in
	// order.
	require.Eventually(t, func() bool {
		return len(outboxMessages(t, root)) == 2
	}, 5*time.Second, 10*time.Millisecond)
	replies := outboxMessages(t, root)
	require.Len(t, replies, 2)
	assert.Equal(t, "reply-1", replies[0].Content)
	assert.Equal(t, "reply-2", replies[1].Content)
	assert.Equal(t, message.Message{Channel: "http", Target: "sender", Content: "reply-1"}, replies[0])

	// The turn's memory write landed in the agent's memory store.
	_, err := os.Stat(filepath.Join(root, "memory.db"))
	require.NoError(t, err)

	// Curation-only storage (ADR 0006): nothing is captured at accept
	// time — the raw inbound messages never enter memory, active or
	// archived; only the writeback does.
	assert.NotContains(t, allMemoryContents(t, root), "first",
		"the first inbound message is never auto-stored")
	assert.NotContains(t, allMemoryContents(t, root), "second",
		"the second inbound message is never auto-stored")

	// The SSE subscriber observed the loop events of both turns, in
	// order: turn 1 fully finishes before turn 2 starts.
	got := collectEvents(t, events, 10)
	var kinds []string
	var startedTasks []string
	for _, e := range got {
		kinds = append(kinds, e["kind"].(string))
		if e["kind"] == "turn_started" {
			startedTasks = append(startedTasks, e["task"].(string))
		}
	}
	require.Len(t, startedTasks, 2)
	assert.Contains(t, startedTasks[0], "] unknown: first",
		"the turn input renders the message with its ID and sender (ADR 0015)")
	assert.Contains(t, startedTasks[1], "] unknown: second")
	// A recall event per turn: #630 wired the read step, so every turn
	// resolves its task into an address before the act loop (ADR 0019).
	// The writeback fires once per turn, when the turn ends on the
	// reserved call, rather than once per model response (issue #615).
	assert.Equal(t, []string{
		"turn_started", "recall", "tool_call", "writeback", "turn_finished",
		"turn_started", "recall", "tool_call", "writeback", "turn_finished",
	}, kinds)

	// One agent per process: a message naming another agent ID gets 404.
	resp = postJSON(t, srv.URL+"/agents/env-2/messages", `{"content":"quiet"}`)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	resp.Body.Close()
}

// allMemoryContents returns the address and value of every memory in the
// agent's store, active or archived — the platform never hard-deletes.
func allMemoryContents(t *testing.T, root string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(root, "memory.db"))
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, db.Close())
	}()
	rows, err := db.Query(
		`SELECT subject || ' ' || question || ' ' || restriction || ' ' || value
		 FROM memories_active
		 UNION ALL
		 SELECT subject || ' ' || question || ' ' || restriction || ' ' || value
		 FROM memories_archive`)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, rows.Close())
	}()
	var contents []string
	for rows.Next() {
		var content string
		require.NoError(t, rows.Scan(&content))
		contents = append(contents, content)
	}
	require.NoError(t, rows.Err())
	return contents
}

func postJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	require.NoError(t, err)
	return resp
}

// subscribeSSE opens the event stream and returns a channel of decoded
// events. The stream is closed by test cleanup.
func subscribeSSE(t *testing.T, url string) chan map[string]any {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	t.Cleanup(func() { resp.Body.Close() })

	out := make(chan map[string]any, 64)
	go func() {
		defer close(out)
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line, ok := strings.CutPrefix(scanner.Text(), "data: ")
			if !ok {
				continue
			}
			var e map[string]any
			if err := json.Unmarshal([]byte(line), &e); err == nil {
				out <- e
			}
		}
	}()
	return out
}

// collectEvents drains n events from the stream or fails after a timeout.
func collectEvents(t *testing.T, events chan map[string]any, n int) []map[string]any {
	t.Helper()
	var out []map[string]any
	deadline := time.After(5 * time.Second)
	for len(out) < n {
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatalf("event stream closed after %d events, want %d", len(out), n)
			}
			out = append(out, e)
		case <-deadline:
			t.Fatalf("timed out after %d events, want %d", len(out), n)
		}
	}
	return out
}

// outboxWriter stands in for the message_send tool: it writes one JSON
// file per message into the outbox directory, named so a reader sees
// delivery order. Production has no such type — the tool is a separate
// binary and the write is the delivery (ADR 0007) — so these scenarios
// drive the tool's contract without spawning it.
type outboxWriter struct {
	dir string
	mu  sync.Mutex
	seq int
}

func newOutboxWriter(root string) *outboxWriter {
	return &outboxWriter{dir: filepath.Join(root, gateway.OutboxDir)}
}

func (o *outboxWriter) Send(_ context.Context, msg message.Message) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	o.mu.Lock()
	o.seq++
	name := fmt.Sprintf("%d-%06d.json", time.Now().UnixNano(), o.seq)
	o.mu.Unlock()
	if err := os.MkdirAll(o.dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(o.dir, name), payload, 0o644)
}

// outboxMessages reads the agent's outbox (root/outbox) in delivery
// order.
func outboxMessages(t *testing.T, root string) []message.Message {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, gateway.OutboxDir))
	if err != nil {
		return nil
	}
	var out []message.Message
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(root, gateway.OutboxDir, e.Name()))
		require.NoError(t, err)
		var msg message.Message
		require.NoError(t, json.Unmarshal(raw, &msg))
		out = append(out, msg)
	}
	return out
}

// writebackStep scripts the call a turn ends with: the reserved
// writeback tool carrying the response document (issue #615).
func writebackStep(document string) agent.ScriptedStep {
	document = strings.TrimSuffix(document, "}") + `,"working_context":` + string(fixtureCheckpoint("active").ToolCalls[0].Input) + `}`
	return agent.ScriptedStep{ToolCalls: []agent.ToolCall{{
		Name:  agent.MemoryWriteTool,
		Input: json.RawMessage(document),
	}}}
}

// resolveStep scripts the read step's model call, which every turn now
// makes before the act loop (ADR 0019). Content and no tool call is the
// step placing no address, which is what a turn whose task names no
// stored fact resolves to.
func resolveStep() agent.ScriptedStep {
	return agent.ScriptedStep{Content: "no address"}
}
