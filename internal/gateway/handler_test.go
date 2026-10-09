package gateway_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/gateway"
	"github.com/garamsh/sherlock/internal/tool"
)

// newTestServer wires the one agent's queue over a fake factory into an
// httptest server. The queue serves agent "env-1".
func newTestServer(t *testing.T, newFn func(agentID string, events agent.EventSink) (*gateway.Runtime, error)) *httptest.Server {
	t.Helper()
	q, err := gateway.NewQueue(&fakeFactory{newFn: newFn}, "env-1", newTestLogger())
	require.NoError(t, err)
	t.Cleanup(func() { q.Close() })
	srv := httptest.NewServer(gateway.NewHandler(q, noIssuers(), gateway.Instance{}, newTestLogger()))
	t.Cleanup(srv.Close)
	return srv
}

func postMessage(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	require.NoError(t, err)
	return resp
}

func TestServer_PostMessageReturns202WithID(t *testing.T) {
	srv := newTestServer(t, func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{Session: &fakeSession{}, Close: func(context.Context) error { return nil }}, nil
	})

	resp := postMessage(t, srv.URL+"/agents/env-1/messages", `{"content":"hello"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)

	var ack struct {
		ID      string `json:"id"`
		AgentID string `json:"agent_id"`
		Status  string `json:"status"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&ack))
	assert.NotEmpty(t, ack.ID)
	assert.Equal(t, "env-1", ack.AgentID)
	assert.Equal(t, "queued", ack.Status)
}

func TestServer_PostMessageRejectsBadRequests(t *testing.T) {
	srv := newTestServer(t, func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{Session: &fakeSession{}, Close: func(context.Context) error { return nil }}, nil
	})

	tests := []struct {
		name   string
		url    string
		body   string
		status int
	}{
		{"invalid JSON", "/agents/env-1/messages", `{"content":`, http.StatusBadRequest},
		{"empty content", "/agents/env-1/messages", `{"content":""}`, http.StatusBadRequest},
		{"unknown field", "/agents/env-1/messages", `{"content":"hi","extra":1}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := postMessage(t, srv.URL+tt.url, tt.body)
			defer resp.Body.Close()
			assert.Equal(t, tt.status, resp.StatusCode)
		})
	}
}

// TestServer_PostMessagePlainHTTPHasNoSender pins the sender source
// (ADR 0015): over plain HTTP there is no caller identity, so the
// envelope's sender is empty and the turn renders it as "unknown".
func TestServer_PostMessagePlainHTTPHasNoSender(t *testing.T) {
	session := &fakeSession{}
	srv := newTestServer(t, func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{Session: session, Close: func(context.Context) error { return nil }}, nil
	})

	resp := postMessage(t, srv.URL+"/agents/env-1/messages", `{"content":"hello"}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	require.Eventually(t, func() bool {
		return len(session.startedTasks()) == 1
	}, 5*time.Second, time.Millisecond)
	assert.Contains(t, session.startedTasks()[0], "unknown: hello")
}

// TestServer_MismatchedAgentIDGets404 pins one-agent-per-process
// (ADR 0017): the HTTP paths keep the /agents/{agent-id}/ shape, but a
// request naming any other agent ID gets 404.
func TestServer_MismatchedAgentIDGets404(t *testing.T) {
	srv := newTestServer(t, func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{Session: &fakeSession{}, Close: func(context.Context) error { return nil }}, nil
	})

	resp := postMessage(t, srv.URL+"/agents/someone-else/messages", `{"content":"hello"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp, err := http.Get(srv.URL + "/agents/someone-else/events")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// TestServer_GRNAgentIDRoutesVerbatim holds that the agent ID is opaque
// (ADR 0016): a GRN, colons included, is the path segment both routes
// answer on, and an ID differing only in its final segment gets 404.
func TestServer_GRNAgentIDRoutesVerbatim(t *testing.T) {
	const grn = "grn:garam:default:agent:144460a71649d1a1"
	const other = "grn:garam:default:agent:0000000000000000"
	factory := &fakeFactory{newFn: func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{Session: &fakeSession{}, Close: func(context.Context) error { return nil }}, nil
	}}
	q, err := gateway.NewQueue(factory, grn, newTestLogger())
	require.NoError(t, err)
	t.Cleanup(func() { q.Close() })
	srv := httptest.NewServer(gateway.NewHandler(q, noIssuers(), gateway.Instance{}, newTestLogger()))
	t.Cleanup(srv.Close)

	eventsStatus := func(id string) int {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/agents/"+id+"/events", nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		return resp.StatusCode
	}

	resp := postMessage(t, srv.URL+"/agents/"+grn+"/messages", `{"content":"hello"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	var ack struct {
		AgentID string `json:"agent_id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&ack))
	assert.Equal(t, grn, ack.AgentID)
	assert.Equal(t, http.StatusOK, eventsStatus(grn))

	resp = postMessage(t, srv.URL+"/agents/"+other+"/messages", `{"content":"hello"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, http.StatusNotFound, eventsStatus(other))
}

func TestServer_MethodNotAllowed(t *testing.T) {
	srv := newTestServer(t, func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{Session: &fakeSession{}, Close: func(context.Context) error { return nil }}, nil
	})
	resp, err := http.Get(srv.URL + "/agents/env-1/messages")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

// TestServer_LegacyEnvPathsAreGone pins ADR 0016: the API is
// agent-addressed; the old /envs/ paths serve nothing.
func TestServer_LegacyEnvPathsAreGone(t *testing.T) {
	srv := newTestServer(t, func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{Session: &fakeSession{}, Close: func(context.Context) error { return nil }}, nil
	})

	resp := postMessage(t, srv.URL+"/envs/env-1/messages", `{"content":"hello"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp, err := http.Get(srv.URL + "/envs/env-1/events")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// sseListener subscribes to the event stream and decodes every event into
// a channel until the request context ends.
func sseListener(t *testing.T, url string) chan map[string]any {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	t.Cleanup(func() { resp.Body.Close() })

	events := make(chan map[string]any, 64)
	go func() {
		defer close(events)
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line, ok := strings.CutPrefix(scanner.Text(), "data: ")
			if !ok {
				continue
			}
			var e map[string]any
			if err := json.Unmarshal([]byte(line), &e); err == nil {
				events <- e
			}
		}
	}()
	return events
}

func TestServer_EventsStreamsTurnEvents(t *testing.T) {
	var mu sync.Mutex
	var sink agent.EventSink
	srv := newTestServer(t, func(_ string, events agent.EventSink) (*gateway.Runtime, error) {
		mu.Lock()
		sink = events
		mu.Unlock()
		session := &emittingSession{events: events}
		return &gateway.Runtime{Session: session, Close: func(context.Context) error { return nil }}, nil
	})

	received := sseListener(t, srv.URL+"/agents/env-1/events")

	resp := postMessage(t, srv.URL+"/agents/env-1/messages", `{"content":"hello"}`)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	var ack struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&ack))
	resp.Body.Close()

	// The subscriber sees the loop events the session emitted; the
	// terminal event references the drained message IDs (ADR 0015).
	var kinds []string
	var terminal map[string]any
	deadline := time.After(5 * time.Second)
	for len(kinds) < 2 {
		select {
		case e := <-received:
			assert.Equal(t, "env-1", e["agent_id"])
			kinds = append(kinds, e["kind"].(string))
			terminal = e
		case <-deadline:
			t.Fatalf("timed out waiting for events, got %v", kinds)
		}
	}
	assert.Equal(t, []string{string(agent.EventTurnStarted), string(agent.EventTurnFinished)}, kinds)
	assert.Equal(t, []any{ack.ID}, terminal["message_ids"],
		"the terminal event references the drained batch's message IDs")

	mu.Lock()
	require.NotNil(t, sink)
	mu.Unlock()
}

func TestServer_EventsStreamsTurnFailed(t *testing.T) {
	srv := newTestServer(t, func(_ string, events agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{
			Session: &failingSession{events: events},
			Close:   func(context.Context) error { return nil },
		}, nil
	})

	received := sseListener(t, srv.URL+"/agents/env-1/events")

	resp := postMessage(t, srv.URL+"/agents/env-1/messages", `{"content":"hello"}`)
	resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	// A failed run ends the subscriber's view of the turn: turn_failed is
	// the terminal event and carries the error.
	var kinds []string
	var terminal map[string]any
	deadline := time.After(5 * time.Second)
	for len(kinds) < 2 {
		select {
		case e := <-received:
			assert.Equal(t, "env-1", e["agent_id"])
			kinds = append(kinds, e["kind"].(string))
			terminal = e
		case <-deadline:
			t.Fatalf("timed out waiting for events, got %v", kinds)
		}
	}
	assert.Equal(t, []string{string(agent.EventTurnStarted), string(agent.EventTurnFailed)}, kinds)
	assert.Contains(t, terminal["error"], "model chat")
}

// TestServer_EventsWritebackNoteDistinguishable pins what the monitoring
// stream says about the note: a cleared note arrives as an empty string,
// and an unchanged one carries no note field at all.
func TestServer_EventsWritebackNoteDistinguishable(t *testing.T) {
	srv := newTestServer(t, func(_ string, events agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{
			Session: &writebackSession{events: events},
			Close:   func(context.Context) error { return nil },
		}, nil
	})

	received := sseListener(t, srv.URL+"/agents/env-1/events")

	resp := postMessage(t, srv.URL+"/agents/env-1/messages", `{"content":"hello"}`)
	resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	cleared, unchanged := readWritebackEvents(t, received)
	assert.Equal(t, "", cleared["note"], "a cleared note serializes as an empty string")
	_, present := unchanged["note"]
	assert.False(t, present, "an unchanged note carries no note field")
}

// readWritebackEvents drains events until the cleared-note and
// unchanged-note writeback events have both arrived.
func readWritebackEvents(t *testing.T, received <-chan map[string]any) (cleared, unchanged map[string]any) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for cleared == nil || unchanged == nil {
		select {
		case e := <-received:
			if e["kind"] != string(agent.EventWriteback) {
				continue
			}
			if _, present := e["note"]; present {
				cleared = e
			} else {
				unchanged = e
			}
		case <-deadline:
			t.Fatal("timed out waiting for both writeback events")
		}
	}
	return cleared, unchanged
}

// writebackSession emits the two writeback shapes a real session
// produces: a note saved as empty, and a note left unchanged.
type writebackSession struct {
	events agent.EventSink
}

func (s *writebackSession) Run(ctx context.Context, task string) (string, error) {
	empty := ""
	s.events.Emit(ctx, agent.Event{Kind: agent.EventTurnStarted, Turn: 1, Task: task})
	s.events.Emit(ctx, agent.Event{Kind: agent.EventWriteback, Turn: 1, Note: &empty})
	s.events.Emit(ctx, agent.Event{Kind: agent.EventWriteback, Turn: 1})
	s.events.Emit(ctx, agent.Event{Kind: agent.EventTurnFinished, Turn: 1, Response: "done"})
	return "done", nil
}

func (s *writebackSession) History() []agent.Message { return nil }

// failingSession publishes turn_started and turn_failed per run,
// mimicking the real session's event contract for a failed run.
type failingSession struct {
	events agent.EventSink
}

func (s *failingSession) Run(ctx context.Context, task string) (string, error) {
	s.events.Emit(ctx, agent.Event{Kind: agent.EventTurnStarted, Turn: 1, Task: task})
	err := errors.New("model chat: provider down")
	s.events.Emit(ctx, agent.Event{Kind: agent.EventTurnFailed, Turn: 1, Error: err.Error()})
	return "", err
}

func (s *failingSession) History() []agent.Message { return nil }

// emittingSession publishes one turn_started and one turn_finished event
// per run, mimicking the real session's event contract.
type emittingSession struct {
	events agent.EventSink
}

func (s *emittingSession) Run(ctx context.Context, task string) (string, error) {
	s.events.Emit(ctx, agent.Event{Kind: agent.EventTurnStarted, Turn: 1, Task: task})
	s.events.Emit(ctx, agent.Event{Kind: agent.EventTurnFinished, Turn: 1, Response: "done"})
	return "done", nil
}

func (s *emittingSession) History() []agent.Message { return nil }

// heldModel holds every act call until the test answers it through turn:
// nil ends the turn with a writeback, an error fails it. It records each
// act call's turn input; summaryModel answers its summary calls.
type heldModel struct {
	turns chan error

	mu     sync.Mutex
	inputs []string
}

func newHeldModel() *heldModel { return &heldModel{turns: make(chan error)} }

func (m *heldModel) Chat(ctx context.Context, messages []agent.Message, _ []tool.Descriptor) (agent.Response, error) {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == agent.RoleUser {
			m.mu.Lock()
			m.inputs = append(m.inputs, messages[i].Content)
			m.mu.Unlock()
			break
		}
	}
	select {
	case err := <-m.turns:
		if err != nil {
			return agent.Response{}, err
		}
		return agent.Response{ToolCalls: writebackStep(`{"reply":"Noted.","note":null}`).ToolCalls}, nil
	case <-ctx.Done():
		return agent.Response{}, ctx.Err()
	}
}

// started returns the input of every act call so far.
func (m *heldModel) started() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.inputs...)
}

// turn answers the act call of the next running turn.
func (m *heldModel) turn(t *testing.T, err error) {
	t.Helper()
	select {
	case m.turns <- err:
	case <-time.After(5 * time.Second):
		t.Fatal("no turn reached the model")
	}
}

// newMemoryQueue serves agent "env-1" over the real runtime: a memory
// store at path and model behind the reasoning loop.
func newMemoryQueue(t *testing.T, path string, model *heldModel) *gateway.Queue {
	t.Helper()
	q, err := gateway.NewQueue(&gateway.Factory{
		MemoryPath: path,
		NewModel:   func(string) agent.Model { return &summaryModel{inner: model} },
		Embedder:   wordEmbedder{},
		Tools:      noTools(),
	}, "env-1", newTestLogger())
	require.NoError(t, err)
	t.Cleanup(func() { q.Close() })
	return q
}

func newMemoryServer(t *testing.T, model *heldModel) *httptest.Server {
	t.Helper()
	q := newMemoryQueue(t, filepath.Join(t.TempDir(), "memory.db"), model)
	srv := httptest.NewServer(gateway.NewHandler(q, noIssuers(), gateway.Instance{}, newTestLogger()))
	t.Cleanup(srv.Close)
	return srv
}

// messageReply is the decoded body of a message POST or lookup.
type messageReply struct {
	ID      string `json:"id"`
	AgentID string `json:"agent_id"`
	Status  string `json:"status"`
	Error   string `json:"error"`
}

func sendMessage(t *testing.T, client *http.Client, base, body string) (int, messageReply) {
	t.Helper()
	resp, err := client.Post(base+"/agents/env-1/messages", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	var reply messageReply
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&reply))
	return resp.StatusCode, reply
}

func lookupMessage(t *testing.T, client *http.Client, base, id string) string {
	t.Helper()
	status, err := fetchStatus(client, base, id)
	require.NoError(t, err)
	return status
}

// fetchStatus looks the message up and checks the reply names the ID it
// answers for, which the caller verifies.
func fetchStatus(client *http.Client, base, id string) (string, error) {
	resp, err := client.Get(base + "/agents/env-1/messages/" + url.PathEscape(id))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("lookup %s: status %d", id, resp.StatusCode)
	}
	var reply messageReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return "", err
	}
	if reply.ID != id {
		return "", fmt.Errorf("lookup %s answered for %s", id, reply.ID)
	}
	return reply.Status, nil
}

func awaitStatus(t *testing.T, client *http.Client, base, id, status string) {
	t.Helper()
	require.Eventually(t, func() bool {
		got, err := fetchStatus(client, base, id)
		return err == nil && got == status
	}, 5*time.Second, 5*time.Millisecond, "message %s never became %s", id, status)
}

func awaitStarted(t *testing.T, model *heldModel, n int) {
	t.Helper()
	require.Eventually(t, func() bool {
		return len(model.started()) == n
	}, 5*time.Second, time.Millisecond, "turn %d never started", n)
}

// TestServer_ReplayOfCommittedMessageCompletesWithoutATurn pins the
// committed-replay row: 200 completed, and no turn runs for it again.
func TestServer_ReplayOfCommittedMessageCompletesWithoutATurn(t *testing.T) {
	model := newHeldModel()
	srv := newMemoryServer(t, model)
	code, reply := sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m1","content":"hello"}`)
	require.Equal(t, http.StatusAccepted, code)
	require.Equal(t, messageReply{ID: "m1", AgentID: "env-1", Status: "queued"}, reply, "the sender's ID comes back byte for byte")
	model.turn(t, nil)
	awaitStatus(t, http.DefaultClient, srv.URL, "m1", "completed")

	code, reply = sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m1","content":"hello"}`)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, messageReply{ID: "m1", AgentID: "env-1", Status: "completed"}, reply)

	// A later message runs, so an m1 queued again would have run with it
	// or before it.
	code, _ = sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m2","content":"later"}`)
	require.Equal(t, http.StatusAccepted, code)
	model.turn(t, nil)
	awaitStatus(t, http.DefaultClient, srv.URL, "m2", "completed")
	assert.Equal(t, 1, strings.Count(strings.Join(model.started(), "\n"), "[m1]"), "m1 ran once")
}

// TestServer_ReplayOfQueuedMessageIsNotEnqueuedAgain pins the queued-replay
// row: 202 queued, and the message is queued once.
func TestServer_ReplayOfQueuedMessageIsNotEnqueuedAgain(t *testing.T) {
	model := newHeldModel()
	srv := newMemoryServer(t, model)
	code, _ := sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m0","content":"first"}`)
	require.Equal(t, http.StatusAccepted, code)
	awaitStarted(t, model, 1)
	code, _ = sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m1","content":"second"}`)
	require.Equal(t, http.StatusAccepted, code)
	require.Equal(t, "pending", lookupMessage(t, http.DefaultClient, srv.URL, "m1"), "m1 waits behind the running turn")

	code, reply := sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m1","content":"second"}`)
	assert.Equal(t, http.StatusAccepted, code)
	assert.Equal(t, messageReply{ID: "m1", AgentID: "env-1", Status: "queued"}, reply)

	model.turn(t, nil)
	model.turn(t, nil)
	awaitStatus(t, http.DefaultClient, srv.URL, "m1", "completed")
	// A later message runs, so a second m1 would have run with it or
	// before it.
	code, _ = sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m2","content":"later"}`)
	require.Equal(t, http.StatusAccepted, code)
	model.turn(t, nil)
	awaitStatus(t, http.DefaultClient, srv.URL, "m2", "completed")
	assert.Equal(t, 1, strings.Count(strings.Join(model.started(), "\n"), "[m1]"), "m1 ran once")
}

// TestServer_ReplayOfQueuedMessageWithOtherContentIsRefused pins the
// mismatch row for a queued message; the same replay with the original
// content is the control.
func TestServer_ReplayOfQueuedMessageWithOtherContentIsRefused(t *testing.T) {
	model := newHeldModel()
	srv := newMemoryServer(t, model)
	code, _ := sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m0","content":"first"}`)
	require.Equal(t, http.StatusAccepted, code)
	awaitStarted(t, model, 1)
	code, _ = sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m1","content":"second"}`)
	require.Equal(t, http.StatusAccepted, code)

	code, reply := sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m1","content":"other"}`)
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, messageReply{Error: "id reused with different content"}, reply)
	code, _ = sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m1","content":"second"}`)
	assert.Equal(t, http.StatusAccepted, code, "the original content is still accepted")

	model.turn(t, nil)
	model.turn(t, nil)
	awaitStatus(t, http.DefaultClient, srv.URL, "m1", "completed")
	assert.NotContains(t, strings.Join(model.started(), "\n"), "other", "the refused content never ran")
}

// TestServer_ReplayOfCommittedMessageWithOtherContentIsRefused pins the
// mismatch row for a committed message; the same replay with the original
// content is the control.
func TestServer_ReplayOfCommittedMessageWithOtherContentIsRefused(t *testing.T) {
	model := newHeldModel()
	srv := newMemoryServer(t, model)
	code, _ := sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m1","content":"hello"}`)
	require.Equal(t, http.StatusAccepted, code)
	model.turn(t, nil)
	awaitStatus(t, http.DefaultClient, srv.URL, "m1", "completed")

	code, reply := sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m1","content":"other"}`)
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, messageReply{Error: "id reused with different content"}, reply)
	code, _ = sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m1","content":"hello"}`)
	assert.Equal(t, http.StatusOK, code, "the original content still completes")
}

// TestServer_MessageCompletesOnlyAfterCommit pins that completed is the
// committed sources: a running turn's message is pending, and a turn that
// ends without committing leaves its message unknown and deliverable.
func TestServer_MessageCompletesOnlyAfterCommit(t *testing.T) {
	model := newHeldModel()
	srv := newMemoryServer(t, model)
	code, _ := sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m1","content":"hello"}`)
	require.Equal(t, http.StatusAccepted, code)
	awaitStarted(t, model, 1)
	assert.Equal(t, "pending", lookupMessage(t, http.DefaultClient, srv.URL, "m1"), "a running turn has committed nothing")
	code, reply := sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m1","content":"hello"}`)
	assert.Equal(t, http.StatusAccepted, code)
	assert.Equal(t, "queued", reply.Status)
	model.turn(t, nil)
	awaitStatus(t, http.DefaultClient, srv.URL, "m1", "completed")

	code, _ = sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m2","content":"lost"}`)
	require.Equal(t, http.StatusAccepted, code)
	model.turn(t, errors.New("model unavailable"))
	awaitStatus(t, http.DefaultClient, srv.URL, "m2", "unknown")
	code, reply = sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m2","content":"lost"}`)
	assert.Equal(t, http.StatusAccepted, code, "an uncommitted message is delivered again")
	assert.Equal(t, "queued", reply.Status)
	model.turn(t, nil)
	awaitStatus(t, http.DefaultClient, srv.URL, "m2", "completed")
}

// TestServer_RestartForgetsPendingAndKeepsCompleted pins that pending
// lives in the process and completed in memory.db: a message still queued
// when the queue closes reads unknown after the restart, while the turn
// in flight completes through the drain and stays completed.
func TestServer_RestartForgetsPendingAndKeepsCompleted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	model := newHeldModel()
	q := newMemoryQueue(t, path, model)
	srv := httptest.NewServer(gateway.NewHandler(q, noIssuers(), gateway.Instance{}, newTestLogger()))
	t.Cleanup(srv.Close)
	code, _ := sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m1","content":"kept"}`)
	require.Equal(t, http.StatusAccepted, code)
	model.turn(t, nil)
	awaitStatus(t, http.DefaultClient, srv.URL, "m1", "completed")
	code, _ = sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m2","content":"in flight"}`)
	require.Equal(t, http.StatusAccepted, code)
	awaitStarted(t, model, 2)
	code, _ = sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m3","content":"queued"}`)
	require.Equal(t, http.StatusAccepted, code)
	require.Equal(t, "pending", lookupMessage(t, http.DefaultClient, srv.URL, "m3"))

	closed := make(chan error, 1)
	go func() { closed <- q.Close() }()
	require.Eventually(t, q.Draining, 5*time.Second, time.Millisecond)
	model.turn(t, nil)
	require.NoError(t, <-closed)

	restarted := httptest.NewServer(gateway.NewHandler(newMemoryQueue(t, path, newHeldModel()), noIssuers(), gateway.Instance{}, newTestLogger()))
	t.Cleanup(restarted.Close)
	assert.Equal(t, "completed", lookupMessage(t, http.DefaultClient, restarted.URL, "m1"))
	assert.Equal(t, "completed", lookupMessage(t, http.DefaultClient, restarted.URL, "m2"))
	assert.Equal(t, "unknown", lookupMessage(t, http.DefaultClient, restarted.URL, "m3"))
}

// TestServer_LookupKeepsSenderIDByteForByte pins that an ID is not
// normalized: one with a slash and spaces reads back as sent.
func TestServer_LookupKeepsSenderIDByteForByte(t *testing.T) {
	model := newHeldModel()
	srv := newMemoryServer(t, model)
	id := "garam/agent-7 msg:42?x"
	code, reply := sendMessage(t, http.DefaultClient, srv.URL, `{"id":"garam/agent-7 msg:42?x","content":"hello"}`)
	require.Equal(t, http.StatusAccepted, code)
	assert.Equal(t, id, reply.ID)
	model.turn(t, nil)
	awaitStatus(t, http.DefaultClient, srv.URL, id, "completed")
	assert.Equal(t, "unknown", lookupMessage(t, http.DefaultClient, srv.URL, "garam/agent-7 msg:42"), "a prefix of the ID is another ID")
}

// TestServer_StatusReportsTheInstanceAndItsState holds C3: the status
// route answers contract version 1 with the agent ID and the instance it
// was given, serving until the drain begins and draining after. A
// request naming another agent gets 404.
func TestServer_StatusReportsTheInstanceAndItsState(t *testing.T) {
	q := newTestQueue(t, &fakeSession{})
	started := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(gateway.NewHandler(q, noIssuers(), gateway.Instance{
		AssignmentEpoch: "epoch-7",
		Generation:      "0123456789abcdef0123456789abcdef",
		ConfigRevision:  "rev-42",
		StartedAt:       started,
	}, newTestLogger()))
	t.Cleanup(srv.Close)

	reply, err := fetchAgentStatus(srv.URL)
	require.NoError(t, err)
	assert.Equal(t, statusReply{
		ContractVersion: 1,
		AgentID:         "env-1",
		AssignmentEpoch: "epoch-7",
		Generation:      "0123456789abcdef0123456789abcdef",
		ConfigRevision:  "rev-42",
		State:           "serving",
		StartedAt:       started,
	}, reply)

	q.Drain()
	reply, err = fetchAgentStatus(srv.URL)
	require.NoError(t, err)
	assert.Equal(t, "draining", reply.State)

	resp, err := http.Get(srv.URL + "/agents/someone-else/status")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
