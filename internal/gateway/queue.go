package gateway

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/garamsh/sherlock/internal/agent"
)

// DefaultQueueCapacity bounds the inbox. Enqueue blocks when
// the queue is full, giving senders natural backpressure.
const DefaultQueueCapacity = 64

// RuntimeFactory builds the agent's runtime. The events sink is the
// agent's monitoring stream and outlives any single runtime.
type RuntimeFactory interface {
	New(ctx context.Context, agentID string, events agent.EventSink) (*Runtime, error)
}

// Queue is the gateway's core: it serializes the one agent's runs so the
// agent runs one turn at a time (ADR 0007, ADR 0017 — one agent per
// process). Queue is safe for concurrent use; Close shuts the runner and
// the runtime down.
type Queue struct {
	agentID string
	runtime *Runtime
	broker  *Broker
	inbox   chan Inbound
	logger  *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	once   sync.Once
	closed chan struct{}

	// mu guards pending: the content of every message queued or running
	// in this process, by sender and ID. It is held from the check to the
	// insert, so one ID is never admitted twice.
	mu      sync.Mutex
	pending map[messageKey]string
}

// messageKey scopes a message ID to the sender that delivered it.
type messageKey struct{ sender, id string }

// NewQueue builds the agent's runtime through factory and starts the
// runner. A factory error fails startup: the runtime is created eagerly
// so a misconfiguration surfaces before the gateway serves. A nil logger
// discards log output.
func NewQueue(factory RuntimeFactory, agentID string, logger *slog.Logger) (*Queue, error) {
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	broker := NewBroker()
	events := &batchEvents{inner: broker}
	rt, err := factory.New(ctx, agentID, events)
	if err != nil {
		cancel()
		broker.Close()
		return nil, err
	}
	q := &Queue{
		agentID: agentID,
		runtime: rt,
		broker:  broker,
		inbox:   make(chan Inbound, DefaultQueueCapacity),
		logger:  logger,
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
		closed:  make(chan struct{}),
		pending: make(map[messageKey]string),
	}
	go q.run(events)
	return q, nil
}

// AgentID returns the ID of the agent this process serves.
func (q *Queue) AgentID() string { return q.agentID }

// Enqueue queues the sender's message under id, or under an ID it
// assigns when id is empty, and returns the ID with the message's status;
// there is no synchronous agent response. A message the sender already
// delivered is not queued again: a queued or running one with the same
// content stays StatusPending, a committed one is StatusCompleted, and
// either with different content is refused with ErrIDReused. A full queue
// blocks the caller (honoring ctx) until a run drains a slot. The sender
// is recorded on the envelope so the agent can address replies per
// message (ADR 0015); an empty sender means the channel provided none.
// Nothing is written to memory at accept time: the writeback is the
// single write path (ADR 0006, ADR 0011).
func (q *Queue) Enqueue(ctx context.Context, sender, id, content string) (string, Status, error) {
	select {
	case <-q.closed:
		return "", StatusUnknown, ErrQueueClosed
	default:
	}
	if id == "" {
		assigned, err := newID("msg")
		if err != nil {
			return "", StatusUnknown, err
		}
		id = assigned
	}
	key := messageKey{sender: sender, id: id}
	status, admitted, err := q.admit(ctx, key, content)
	if err != nil {
		return "", StatusUnknown, err
	}
	if !admitted {
		return id, status, nil
	}
	msg := Inbound{ID: id, Sender: sender, Content: content, QueuedAt: time.Now()}
	select {
	case q.inbox <- msg:
		q.logger.Info("message enqueued",
			"op", "enqueue", "agent_id", q.agentID, "message_id", id)
		return id, StatusPending, nil
	case <-ctx.Done():
		q.forget([]messageKey{key})
		return "", StatusUnknown, ctx.Err()
	case <-q.ctx.Done():
		q.forget([]messageKey{key})
		return "", StatusUnknown, ErrQueueClosed
	}
}

// admit records the message as pending and reports it admitted when
// neither this process nor memory holds its key. Otherwise it returns the
// status the key already has, or ErrIDReused when it is held with other
// content.
func (q *Queue) admit(ctx context.Context, key messageKey, content string) (Status, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if held, ok := q.pending[key]; ok {
		if held != content {
			return StatusUnknown, false, ErrIDReused
		}
		return StatusPending, false, nil
	}
	held, same, err := q.committed(ctx, key, content)
	if err != nil {
		return StatusUnknown, false, err
	}
	if held {
		if !same {
			return StatusUnknown, false, ErrIDReused
		}
		return StatusCompleted, false, nil
	}
	q.pending[key] = content
	return StatusPending, true, nil
}

// Status returns where the sender's message ID stands. A message stays
// pending until its turn ends, which is after the turn commits it, so
// a message is never reported unknown between the two.
func (q *Queue) Status(ctx context.Context, sender, id string) (Status, error) {
	key := messageKey{sender: sender, id: id}
	q.mu.Lock()
	_, pending := q.pending[key]
	q.mu.Unlock()
	if pending {
		return StatusPending, nil
	}
	held, _, err := q.committed(ctx, key, "")
	if err != nil {
		return StatusUnknown, err
	}
	if held {
		return StatusCompleted, nil
	}
	return StatusUnknown, nil
}

func (q *Queue) committed(ctx context.Context, key messageKey, content string) (held, same bool, err error) {
	if q.runtime.Committed == nil {
		return false, false, nil
	}
	return q.runtime.Committed(ctx, key.sender, key.id, content)
}

// forget drops the keys of messages no longer queued or running.
func (q *Queue) forget(keys []messageKey) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, key := range keys {
		delete(q.pending, key)
	}
}

// Events returns the agent's monitoring stream.
func (q *Queue) Events() *Broker { return q.broker }

// Close stops accepting messages, cancels an in-flight run, waits for
// the runner to exit, and releases the runtime and the stream. Close is
// idempotent.
func (q *Queue) Close() error {
	var err error
	q.once.Do(func() {
		close(q.closed)
		q.cancel()
		<-q.done
		err = q.runtime.Close(context.Background())
		q.broker.Close()
	})
	return err
}
