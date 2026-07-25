package gateway_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/environment"
	"github.com/garamsh/gagent/internal/gateway"
)

// newTestServer wires a router over a fake factory into an httptest
// server. The returned session receives the events sink of env "env-1"
// through emit.
func newTestServer(t *testing.T, newFn func(envID string, events agent.EventSink) (*gateway.Runtime, error)) *httptest.Server {
	t.Helper()
	router := gateway.NewRouter(&fakeFactory{newFn: newFn}, newTestLogger())
	t.Cleanup(func() { router.Close() })
	srv := httptest.NewServer(gateway.NewHandler(router, newTestLogger()))
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

	resp := postMessage(t, srv.URL+"/envs/env-1/messages", `{"content":"hello"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)

	var ack struct {
		ID     string `json:"id"`
		Env    string `json:"env"`
		Status string `json:"status"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&ack))
	assert.NotEmpty(t, ack.ID)
	assert.Equal(t, "env-1", ack.Env)
	assert.Equal(t, "queued", ack.Status)
}

func TestServer_PostMessageRejectsBadRequests(t *testing.T) {
	srv := newTestServer(t, func(envID string, _ agent.EventSink) (*gateway.Runtime, error) {
		if envID == "bad" {
			return nil, fmt.Errorf("%w: %q", environment.ErrInvalidID, envID)
		}
		return &gateway.Runtime{Session: &fakeSession{}, Close: func(context.Context) error { return nil }}, nil
	})

	tests := []struct {
		name   string
		url    string
		body   string
		status int
	}{
		{"invalid JSON", "/envs/env-1/messages", `{"content":`, http.StatusBadRequest},
		{"empty content", "/envs/env-1/messages", `{"content":""}`, http.StatusBadRequest},
		{"unknown field", "/envs/env-1/messages", `{"content":"hi","extra":1}`, http.StatusBadRequest},
		{"invalid env id", "/envs/bad/messages", `{"content":"hi"}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := postMessage(t, srv.URL+tt.url, tt.body)
			defer resp.Body.Close()
			assert.Equal(t, tt.status, resp.StatusCode)
		})
	}
}

func TestServer_MethodNotAllowed(t *testing.T) {
	srv := newTestServer(t, func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{Session: &fakeSession{}, Close: func(context.Context) error { return nil }}, nil
	})
	resp, err := http.Get(srv.URL + "/envs/env-1/messages")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
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

	received := sseListener(t, srv.URL+"/envs/env-1/events")

	resp := postMessage(t, srv.URL+"/envs/env-1/messages", `{"content":"hello"}`)
	resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	// The subscriber sees the loop events the session emitted.
	var kinds []string
	deadline := time.After(5 * time.Second)
	for len(kinds) < 2 {
		select {
		case e := <-received:
			assert.Equal(t, "env-1", e["env"])
			kinds = append(kinds, e["kind"].(string))
		case <-deadline:
			t.Fatalf("timed out waiting for events, got %v", kinds)
		}
	}
	assert.Equal(t, []string{string(agent.EventTurnStarted), string(agent.EventTurnFinished)}, kinds)

	mu.Lock()
	require.NotNil(t, sink)
	mu.Unlock()
}

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
