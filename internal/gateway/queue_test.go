package gateway_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/gateway"
)

// gatedBatchSession records each turn's input and publishes the real
// session's event contract (turn_started, turn_finished) through the
// sink. A non-nil gate blocks runs until it closes; a closed gate lets
// every later run through.
type gatedBatchSession struct {
	gate   <-chan struct{}
	events agent.EventSink

	mu    sync.Mutex
	tasks []string
}

func (s *gatedBatchSession) Run(ctx context.Context, task string) (string, error) {
	s.mu.Lock()
	s.tasks = append(s.tasks, task)
	s.mu.Unlock()
	s.events.Emit(ctx, agent.Event{Kind: agent.EventTurnStarted, Task: task})
	if s.gate != nil {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	s.events.Emit(ctx, agent.Event{Kind: agent.EventTurnFinished, Response: "done"})
	return "done", nil
}

func (s *gatedBatchSession) History() []agent.Message { return nil }

func (s *gatedBatchSession) startedTasks() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.tasks...)
}

func newBatchQueue(t *testing.T, session *gatedBatchSession) *gateway.Queue {
	t.Helper()
	factory := &fakeFactory{newFn: func(_ string, sink agent.EventSink) (*gateway.Runtime, error) {
		session.events = sink
		return &gateway.Runtime{
			Session: session,
			Capture: noopCapture,
			Close:   func(context.Context) error { return nil },
		}, nil
	}}
	q, err := gateway.NewQueue(factory, "env-1", newTestLogger())
	require.NoError(t, err)
	t.Cleanup(func() { q.Close() })
	return q
}

// TestQueue_DrainsQueuedMessagesIntoOneTurn pins ADR 0016: messages
// queued during an active run are processed in one subsequent turn that
// renders each with its ID and sender.
func TestQueue_DrainsQueuedMessagesIntoOneTurn(t *testing.T) {
	gate := make(chan struct{})
	session := &gatedBatchSession{gate: gate}
	q := newBatchQueue(t, session)

	firstID, err := q.Enqueue(context.Background(), "alice", "first")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return len(session.startedTasks()) == 1
	}, 5*time.Second, time.Millisecond)

	// The first turn is still blocked; these three queue up behind it.
	var ids []string
	for _, m := range []struct{ sender, content string }{
		{"bob", "second"},
		{"carol", "third"},
		{"dave", "fourth"},
	} {
		id, err := q.Enqueue(context.Background(), m.sender, m.content)
		require.NoError(t, err)
		ids = append(ids, id)
	}

	close(gate)
	require.Eventually(t, func() bool {
		return len(session.startedTasks()) == 2
	}, 5*time.Second, time.Millisecond)

	tasks := session.startedTasks()
	assert.Contains(t, tasks[0], firstID)
	batch := tasks[1]
	for i, id := range ids {
		sender := []string{"bob", "carol", "dave"}[i]
		content := []string{"second", "third", "fourth"}[i]
		assert.Contains(t, batch, "["+id+"] "+sender+": "+content,
			"the drained turn renders message %s with its ID and sender", id)
	}
	assert.NotContains(t, batch, firstID,
		"the first turn's message is not re-drained")
}

// TestQueue_DrainIsBounded pins the ADR 0016 bound: a turn never takes
// more than MaxDrainBatch messages; the overflow drains next turn.
func TestQueue_DrainIsBounded(t *testing.T) {
	gate := make(chan struct{})
	session := &gatedBatchSession{gate: gate}
	q := newBatchQueue(t, session)

	_, err := q.Enqueue(context.Background(), "", "blocking")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return len(session.startedTasks()) == 1
	}, 5*time.Second, time.Millisecond)

	queued := gateway.MaxDrainBatch + 2
	for range queued {
		_, err := q.Enqueue(context.Background(), "", "queued")
		require.NoError(t, err)
	}

	close(gate)
	require.Eventually(t, func() bool {
		return len(session.startedTasks()) == 3
	}, 5*time.Second, time.Millisecond)

	tasks := session.startedTasks()
	assert.Equal(t, gateway.MaxDrainBatch, strings.Count(tasks[1], "[msg-"),
		"the second turn drains exactly the bound")
	assert.Equal(t, queued-gateway.MaxDrainBatch, strings.Count(tasks[2], "[msg-"),
		"the overflow drains in the next turn")
}

// TestQueue_TerminalEventReferencesBatchIDs pins ADR 0016: each drain
// ends with exactly one terminal event, and that event references the
// batch's message IDs.
func TestQueue_TerminalEventReferencesBatchIDs(t *testing.T) {
	gate := make(chan struct{})
	session := &gatedBatchSession{gate: gate}
	q := newBatchQueue(t, session)

	stream, cancel := q.Events().Subscribe()
	defer cancel()

	firstID, err := q.Enqueue(context.Background(), "alice", "first")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return len(session.startedTasks()) == 1
	}, 5*time.Second, time.Millisecond)

	var batchIDs []string
	for _, content := range []string{"second", "third"} {
		id, err := q.Enqueue(context.Background(), "bob", content)
		require.NoError(t, err)
		batchIDs = append(batchIDs, id)
	}

	close(gate)

	var started, terminals []agent.Event
	deadline := time.After(5 * time.Second)
	for len(terminals) < 2 {
		select {
		case e := <-stream:
			switch e.Kind {
			case agent.EventTurnStarted:
				started = append(started, e)
			case agent.EventTurnFinished, agent.EventTurnFailed:
				terminals = append(terminals, e)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for terminal events, got %v", terminals)
		}
	}
	require.Len(t, terminals, 2, "exactly one terminal event per drain")
	assert.Len(t, started, 2, "two drains, two turns")
	assert.Equal(t, []string{firstID}, terminals[0].MessageIDs,
		"the first turn's terminal event references its single message")
	assert.Equal(t, batchIDs, terminals[1].MessageIDs,
		"the drained turn's terminal event references the whole batch")
}
