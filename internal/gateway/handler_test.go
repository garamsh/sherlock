package gateway_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/gateway"
)

// newTestServer wires the one agent's queue over a fake factory into an
// httptest server. The queue serves agent "env-1".
func newTestServer(t *testing.T, newFn func(agentID string, events agent.EventSink) (*gateway.Runtime, error)) *httptest.Server {
	t.Helper()
	q, err := gateway.NewQueue(&fakeFactory{newFn: newFn}, "env-1", newTestLogger())
	require.NoError(t, err)
	t.Cleanup(func() { q.Close() })
	srv := httptest.NewServer(gateway.NewHandler(q, newTestLogger()))
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
	srv := httptest.NewServer(gateway.NewHandler(q, newTestLogger()))
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
