package gateway_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/gateway"
)

// fakeFactory builds stub runtimes. A nil newFn yields a stub runtime
// with a no-op session, for tests that never run a turn.
type fakeFactory struct {
	newFn func(agentID string, events agent.EventSink) (*gateway.Runtime, error)
}

func (f *fakeFactory) New(_ context.Context, agentID string, events agent.EventSink) (*gateway.Runtime, error) {
	if f.newFn == nil {
		return &gateway.Runtime{Session: &fakeSession{}, Capture: noopCapture, Close: func(context.Context) error { return nil }}, nil
	}
	return f.newFn(agentID, events)
}

// fakeSession records the order tasks run in. A non-nil gate blocks every
// run until the gate closes (or the run context is canceled).
type fakeSession struct {
	gate <-chan struct{}

	mu      sync.Mutex
	started []string
	failed  []string
	err     error
}

func (s *fakeSession) Run(ctx context.Context, task string) (string, error) {
	s.mu.Lock()
	s.started = append(s.started, task)
	s.mu.Unlock()
	if s.gate != nil {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if s.err != nil {
		s.mu.Lock()
		s.failed = append(s.failed, task)
		s.mu.Unlock()
		return "", s.err
	}
	return "done", nil
}

func (s *fakeSession) History() []agent.Message { return nil }

func (s *fakeSession) startedTasks() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.started...)
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// noopCapture is the no-op inbound-message capture for runtimes whose
// tests do not exercise ADR 0016 capture.
func noopCapture(context.Context, string) error { return nil }

// newTestQueue starts a queue over a fake factory building stub runtimes
// around session; the queue closes with the test.
func newTestQueue(t *testing.T, session *fakeSession) *gateway.Queue {
	t.Helper()
	factory := &fakeFactory{newFn: func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{Session: session, Capture: noopCapture, Close: func(context.Context) error { return nil }}, nil
	}}
	q, err := gateway.NewQueue(factory, "env-1", newTestLogger())
	require.NoError(t, err)
	t.Cleanup(func() { q.Close() })
	return q
}

func TestQueue_EnqueueAssignsIDAndRunsMessage(t *testing.T) {
	session := &fakeSession{}
	q := newTestQueue(t, session)

	id, err := q.Enqueue(context.Background(), "alice", "hello")
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	require.Eventually(t, func() bool {
		return len(session.startedTasks()) == 1
	}, 5*time.Second, time.Millisecond)
	assert.Equal(t, "["+id+"] alice: hello", session.startedTasks()[0],
		"the turn input renders the message with its ID and sender (ADR 0020)")
}

func TestQueue_SerializesRunsFIFO(t *testing.T) {
	gate := make(chan struct{})
	session := &fakeSession{gate: gate}
	q := newTestQueue(t, session)

	_, err := q.Enqueue(context.Background(), "", "first")
	require.NoError(t, err)

	// The first run starts and blocks; the second must not begin
	// mid-run — one active run per agent.
	require.Eventually(t, func() bool {
		return len(session.startedTasks()) == 1
	}, 5*time.Second, time.Millisecond)
	assert.Contains(t, session.startedTasks()[0], "first")

	_, err = q.Enqueue(context.Background(), "", "second")
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	assert.Len(t, session.startedTasks(), 1,
		"the queued followup waits for the active run")

	close(gate)
	require.Eventually(t, func() bool {
		return len(session.startedTasks()) == 2
	}, 5*time.Second, time.Millisecond)
	assert.Contains(t, session.startedTasks()[1], "second")
}

func TestQueue_FactoryErrorFailsStartup(t *testing.T) {
	factory := &fakeFactory{newFn: func(agentID string, _ agent.EventSink) (*gateway.Runtime, error) {
		return nil, fmt.Errorf("build runtime: %s", agentID)
	}}
	_, err := gateway.NewQueue(factory, "env-1", newTestLogger())
	assert.ErrorContains(t, err, "build runtime",
		"a runtime build failure surfaces at startup, not on the first message")
}

func TestQueue_EnqueueAfterCloseFails(t *testing.T) {
	q := newTestQueue(t, &fakeSession{})
	require.NoError(t, q.Close())

	_, err := q.Enqueue(context.Background(), "", "hi")
	assert.ErrorIs(t, err, gateway.ErrQueueClosed)
}

func TestQueue_CloseReleasesRuntime(t *testing.T) {
	var mu sync.Mutex
	var closed bool
	factory := &fakeFactory{newFn: func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{
			Session: &fakeSession{},
			Capture: noopCapture,
			Close: func(context.Context) error {
				mu.Lock()
				closed = true
				mu.Unlock()
				return nil
			},
		}, nil
	}}
	q, err := gateway.NewQueue(factory, "env-1", newTestLogger())
	require.NoError(t, err)

	require.NoError(t, q.Close())
	mu.Lock()
	defer mu.Unlock()
	assert.True(t, closed, "Close releases the agent runtime")
}

func TestQueue_FailedRunDoesNotBlockQueue(t *testing.T) {
	session := &fakeSession{err: errors.New("boom")}
	q := newTestQueue(t, session)

	_, err := q.Enqueue(context.Background(), "", "first")
	require.NoError(t, err)
	_, err = q.Enqueue(context.Background(), "", "second")
	require.NoError(t, err)

	// Both messages are processed even though every turn fails; whether
	// they share one drained turn or run back-to-back is the runner's call.
	require.Eventually(t, func() bool {
		joined := strings.Join(session.startedTasks(), "\n")
		return strings.Contains(joined, "first") && strings.Contains(joined, "second")
	}, 5*time.Second, time.Millisecond)
}

// TestQueue_EnqueueCapturesInboundMessage pins the ADR 0016 capture
// step: every accepted message is captured exactly once, at accept time,
// before its run starts.
func TestQueue_EnqueueCapturesInboundMessage(t *testing.T) {
	gate := make(chan struct{})
	session := &fakeSession{gate: gate}
	var mu sync.Mutex
	var captured []string
	factory := &fakeFactory{newFn: func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{
			Session: session,
			Capture: func(_ context.Context, content string) error {
				mu.Lock()
				captured = append(captured, content)
				mu.Unlock()
				return nil
			},
			Close: func(context.Context) error { return nil },
		}, nil
	}}
	q, err := gateway.NewQueue(factory, "env-1", newTestLogger())
	require.NoError(t, err)
	defer func() {
		close(gate)
		q.Close()
	}()

	_, err = q.Enqueue(context.Background(), "", "first")
	require.NoError(t, err)
	_, err = q.Enqueue(context.Background(), "", "second")
	require.NoError(t, err)

	// The first run is still blocked and the second message has not run,
	// yet both captures already happened — capture precedes the run.
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"first", "second"}, captured)
}

// TestQueue_CaptureFailureRejectsEnqueue pins the failure contract of
// the ADR 0016 capture step: a message whose capture fails is not
// accepted and never runs, so no accepted message goes uncaptured.
func TestQueue_CaptureFailureRejectsEnqueue(t *testing.T) {
	session := &fakeSession{}
	captureErr := errors.New("store unavailable")
	factory := &fakeFactory{newFn: func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{
			Session: session,
			Capture: func(context.Context, string) error {
				return captureErr
			},
			Close: func(context.Context) error { return nil },
		}, nil
	}}
	q, err := gateway.NewQueue(factory, "env-1", newTestLogger())
	require.NoError(t, err)
	defer func() { q.Close() }()

	_, err = q.Enqueue(context.Background(), "", "hi")
	assert.ErrorIs(t, err, captureErr)
	assert.Empty(t, session.startedTasks(), "a rejected message never runs")
}
