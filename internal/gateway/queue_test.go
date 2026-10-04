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

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/gateway"
)

// fakeFactory builds stub runtimes. A nil newFn yields a stub runtime
// with a no-op session, for tests that never run a turn.
type fakeFactory struct {
	newFn func(agentID string, events agent.EventSink) (*gateway.Runtime, error)
}

func (f *fakeFactory) New(_ context.Context, agentID string, events agent.EventSink) (*gateway.Runtime, error) {
	if f.newFn == nil {
		return &gateway.Runtime{Session: &fakeSession{}, Close: func(context.Context) error { return nil }}, nil
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

// newTestQueue starts a queue over a fake factory building stub runtimes
// around session; the queue closes with the test.
func newTestQueue(t *testing.T, session *fakeSession) *gateway.Queue {
	t.Helper()
	factory := &fakeFactory{newFn: func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{Session: session, Close: func(context.Context) error { return nil }}, nil
	}}
	q, err := gateway.NewQueue(factory, "env-1", newTestLogger())
	require.NoError(t, err)
	t.Cleanup(func() { q.Close() })
	return q
}

func TestQueue_EnqueueAssignsIDAndRunsMessage(t *testing.T) {
	session := &fakeSession{}
	q := newTestQueue(t, session)

	id, _, err := q.Enqueue(context.Background(), "alice", "", "hello")
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	require.Eventually(t, func() bool {
		return len(session.startedTasks()) == 1
	}, 5*time.Second, time.Millisecond)
	assert.Equal(t, "["+id+"] alice: hello", session.startedTasks()[0],
		"the turn input renders the message with its ID and sender (ADR 0015)")
}

func TestQueue_SerializesRunsFIFO(t *testing.T) {
	gate := make(chan struct{})
	session := &fakeSession{gate: gate}
	q := newTestQueue(t, session)

	_, _, err := q.Enqueue(context.Background(), "", "", "first")
	require.NoError(t, err)

	// The first run starts and blocks; the second must not begin
	// mid-run — one active run per agent.
	require.Eventually(t, func() bool {
		return len(session.startedTasks()) == 1
	}, 5*time.Second, time.Millisecond)
	assert.Contains(t, session.startedTasks()[0], "first")

	_, _, err = q.Enqueue(context.Background(), "", "", "second")
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

	_, _, err := q.Enqueue(context.Background(), "", "", "hi")
	assert.ErrorIs(t, err, gateway.ErrQueueClosed)
}

func TestQueue_CloseReleasesRuntime(t *testing.T) {
	var mu sync.Mutex
	var closed bool
	factory := &fakeFactory{newFn: func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{
			Session: &fakeSession{},
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

	_, _, err := q.Enqueue(context.Background(), "", "", "first")
	require.NoError(t, err)
	_, _, err = q.Enqueue(context.Background(), "", "", "second")
	require.NoError(t, err)

	// Both messages are processed even though every turn fails; whether
	// they share one drained turn or run back-to-back is the runner's call.
	require.Eventually(t, func() bool {
		joined := strings.Join(session.startedTasks(), "\n")
		return strings.Contains(joined, "first") && strings.Contains(joined, "second")
	}, 5*time.Second, time.Millisecond)
}

// TestQueue_RetentionFailureDoesNotFailTurn pins the failure contract
// for the after-turn passes: the error logs at WARN and the queue keeps
// processing turns.
func TestQueue_RetentionFailureDoesNotFailTurn(t *testing.T) {
	session := &fakeSession{}
	factory := &fakeFactory{newFn: func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{
			Session: session,
			Retain:  func(context.Context) error { return errors.New("store locked") },
			Close:   func(context.Context) error { return nil },
		}, nil
	}}
	q, err := gateway.NewQueue(factory, "env-1", newTestLogger())
	require.NoError(t, err)
	defer func() { q.Close() }()

	_, _, err = q.Enqueue(context.Background(), "", "", "first")
	require.NoError(t, err)
	_, _, err = q.Enqueue(context.Background(), "", "", "second")
	require.NoError(t, err)
	// Both messages are processed despite the failing trigger; whether
	// they share one drained turn or run back-to-back is the runner's call.
	require.Eventually(t, func() bool {
		joined := strings.Join(session.startedTasks(), "\n")
		return strings.Contains(joined, "first") && strings.Contains(joined, "second")
	}, 5*time.Second, time.Millisecond,
		"both messages are processed despite the failing trigger")
}

// TestQueue_RetentionRunsAfterTurn pins the ADR 0006 retention wiring:
// after a completed turn the runner evaluates the Retain pass, off the
// turn path.
func TestQueue_RetentionRunsAfterTurn(t *testing.T) {
	session := &fakeSession{}
	var mu sync.Mutex
	var calls int
	factory := &fakeFactory{newFn: func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{
			Session: session,
			Retain: func(context.Context) error {
				mu.Lock()
				calls++
				mu.Unlock()
				return nil
			},
			Close: func(context.Context) error { return nil },
		}, nil
	}}
	q, err := gateway.NewQueue(factory, "env-1", newTestLogger())
	require.NoError(t, err)
	defer func() { q.Close() }()

	_, _, err = q.Enqueue(context.Background(), "", "", "hi")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return len(session.startedTasks()) == 1
	}, 5*time.Second, time.Millisecond)
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return calls == 1
	}, 5*time.Second, time.Millisecond, "the retention pass fires once, after the turn")
}
