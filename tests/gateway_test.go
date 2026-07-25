// Package tests holds end-to-end scenarios that wire multiple domains
// together (docs/convention/testing.md).
package tests

import (
	"bufio"
	"context"
	"encoding/json"
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

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/message"
	"github.com/garamsh/gagent/internal/sandbox"
	"github.com/garamsh/gagent/internal/tool"
)

// fakeRuntime substitutes the Docker daemon: the scripted scenario never
// calls sandbox_exec, so containers are never created.
type fakeRuntime struct{}

func (fakeRuntime) Create(context.Context, sandbox.Config) (string, error) { return "fake", nil }
func (fakeRuntime) Exec(context.Context, string, []string, time.Duration) (sandbox.Result, error) {
	return sandbox.Result{ExitCode: 0}, nil
}
func (fakeRuntime) Destroy(context.Context, string) error { return nil }

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

// replyScript scripts one full turn per message: message_send with the
// given reply, then a final response carrying a writeback block.
func replyScript(replies ...string) *agent.ScriptedModel {
	var steps []agent.ScriptedStep
	for _, reply := range replies {
		steps = append(steps,
			agent.ScriptedStep{ToolCalls: []agent.ToolCall{{
				Name: "message_send",
				Input: json.RawMessage(
					`{"channel":"http","target":"sender","content":` + strconvQuote(reply) + `}`),
			}}},
			agent.ScriptedStep{Content: "Replied.\n```memory\n" +
				`{"add":[{"content":"handled an inbound message","type":"episodic"}]}` + "\n```"},
		)
	}
	return agent.NewScriptedModel(steps...)
}

func strconvQuote(s string) string {
	q, _ := json.Marshal(s)
	return string(q)
}

func TestGateway_EndToEnd(t *testing.T) {
	root := t.TempDir()
	release := make(chan struct{})
	gated := &gatedModel{inner: replyScript("reply-1", "reply-2"), release: release, started: make(chan struct{})}

	factory := &gateway.Factory{
		Root: root,
		NewModel: func(envID string) agent.Model {
			if envID == "env-1" {
				return gated
			}
			return replyScript("reply-other")
		},
		Sandbox:     fakeRuntime{},
		Image:       "fake-image",
		ExecTimeout: time.Second,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := gateway.NewRouter(factory, logger)
	t.Cleanup(func() { router.Close() })
	srv := httptest.NewServer(gateway.NewHandler(router, logger))
	t.Cleanup(srv.Close)

	// Subscribe to the monitoring stream before any message so no event
	// is missed.
	events := subscribeSSE(t, srv.URL+"/envs/env-1/events")

	// First message: accepted and its run starts (blocking in the model).
	resp := postJSON(t, srv.URL+"/envs/env-1/messages", `{"content":"first"}`)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	resp.Body.Close()
	select {
	case <-gated.started:
	case <-time.After(5 * time.Second):
		t.Fatal("first run did not start")
	}

	// Second message mid-run: queued as a followup, not run yet.
	resp = postJSON(t, srv.URL+"/envs/env-1/messages", `{"content":"second"}`)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	resp.Body.Close()
	assert.Empty(t, outboxMessages(t, root, "env-1"))

	close(release)

	// Both runs complete and deliver their replies to the outbox, in
	// order.
	require.Eventually(t, func() bool {
		return len(outboxMessages(t, root, "env-1")) == 2
	}, 5*time.Second, 10*time.Millisecond)
	replies := outboxMessages(t, root, "env-1")
	require.Len(t, replies, 2)
	assert.Equal(t, "reply-1", replies[0].Content)
	assert.Equal(t, "reply-2", replies[1].Content)
	assert.Equal(t, message.Message{Channel: "http", Target: "sender", Content: "reply-1"}, replies[0])

	// The writeback block landed in the environment's memory store.
	_, err := os.Stat(filepath.Join(root, "env-1", gateway.MemoryDBFile))
	require.NoError(t, err)

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
	assert.Equal(t, []string{"first", "second"}, startedTasks)
	assert.Equal(t, []string{
		"turn_started", "writeback", "tool_call", "writeback", "turn_finished",
		"turn_started", "writeback", "tool_call", "writeback", "turn_finished",
	}, kinds)

	// An environment with no subscribers runs and delivers normally.
	resp = postJSON(t, srv.URL+"/envs/env-2/messages", `{"content":"quiet"}`)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	resp.Body.Close()
	require.Eventually(t, func() bool {
		return len(outboxMessages(t, root, "env-2")) == 1
	}, 5*time.Second, 10*time.Millisecond)
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

// outboxMessages reads the environment's outbox in delivery order.
func outboxMessages(t *testing.T, root, envID string) []message.Message {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, envID, gateway.OutboxDir))
	if err != nil {
		return nil
	}
	var out []message.Message
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(root, envID, gateway.OutboxDir, e.Name()))
		require.NoError(t, err)
		var msg message.Message
		require.NoError(t, json.Unmarshal(raw, &msg))
		out = append(out, msg)
	}
	return out
}
