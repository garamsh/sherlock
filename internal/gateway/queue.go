package gateway

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// DefaultQueueCapacity bounds the inbox. Enqueue blocks when
// the queue is full, giving senders natural backpressure.
const DefaultQueueCapacity = 64

// Queue is the gateway's core: it serializes the one agent's runs so the
// agent runs one turn at a time (ADR 0007, ADR 0017 — one agent per
// process). Queue is safe for concurrent use; Drain stops the runner
// after the turn in flight, and Close then releases the runtime.
type Queue struct {
	agentID string
	runtime *Runtime
	broker  *Broker
	inbox   chan Inbound
	logger  *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	// draining closes when Drain begins: from then on no message is
	// admitted and no queued turn starts.
	draining  chan struct{}
	drainOnce sync.Once
	closeOnce sync.Once

	// mu guards pending: every message queued or running in this
	// process, by scope and ID. It is held from the check to the insert,
	// so one ID is never admitted twice.
	mu      sync.Mutex
	pending map[messageKey]pendingMessage
}

// messageKey scopes a message ID to its origin's issuer and sender.
type messageKey struct{ issuer, sender, id string }

func keyOf(origin Origin, id string) messageKey {
	return messageKey{issuer: origin.issuer, sender: origin.sender, id: id}
}

// pendingMessage is what the queue holds of a message in this process:
// its content, compared on redelivery, and the transport principal that
// posted it, which a lookup answers to.
type pendingMessage struct{ content, poster string }

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
		agentID:  agentID,
		runtime:  rt,
		broker:   broker,
		inbox:    make(chan Inbound, DefaultQueueCapacity),
		logger:   logger,
		ctx:      ctx,
		cancel:   cancel,
		done:     make(chan struct{}),
		draining: make(chan struct{}),
		pending:  make(map[messageKey]pendingMessage),
	}
	go q.run(events)
	return q, nil
}

// AgentID returns the ID of the agent this process serves.
func (q *Queue) AgentID() string { return q.agentID }

// Enqueue queues the message from origin under id, or under an ID it
// assigns when id is empty, and returns the ID with the message's status;
// there is no synchronous agent response. A message the sender already
// delivered is not queued again: a queued or running one with the same
// content stays StatusPending, a committed one is StatusCompleted, and
// either with different content is refused with ErrIDReused. A full queue
// blocks the caller (honoring ctx) until a run drains a slot. The origin
// is recorded on the envelope so the agent can address replies per
// message (ADR 0015, ADR 0026).
// Nothing is written to memory at accept time: the writeback is the
// single write path (ADR 0006, ADR 0011).
func (q *Queue) Enqueue(ctx context.Context, origin Origin, id, content string) (string, Status, error) {
	if q.Draining() {
		return "", StatusUnknown, ErrDraining
	}
	if id == "" {
		assigned, err := newID("msg")
		if err != nil {
			return "", StatusUnknown, err
		}
		id = assigned
	}
	key := keyOf(origin, id)
	status, admitted, err := q.admit(ctx, key, pendingMessage{content: content, poster: origin.poster})
	if err != nil {
		return "", StatusUnknown, err
	}
	if !admitted {
		return id, status, nil
	}
	msg := Inbound{ID: id, Origin: origin, Content: content, QueuedAt: time.Now()}
	select {
	case q.inbox <- msg:
		q.logger.Info("message enqueued",
			"op", "enqueue", "agent_id", q.agentID, "message_id", id)
		return id, StatusPending, nil
	case <-ctx.Done():
		q.forget([]messageKey{key})
		return "", StatusUnknown, ctx.Err()
	case <-q.draining:
		q.forget([]messageKey{key})
		return "", StatusUnknown, ErrDraining
	}
}

// admit records the message as pending and reports it admitted when
// neither this process nor memory holds its key. Otherwise it returns the
// status the key already has, or ErrIDReused when it is held with other
// content.
func (q *Queue) admit(ctx context.Context, key messageKey, msg pendingMessage) (Status, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if held, ok := q.pending[key]; ok {
		if held.content != msg.content {
			return StatusUnknown, false, ErrIDReused
		}
		return StatusPending, false, nil
	}
	held, same, err := q.committed(ctx, key, msg.content)
	if err != nil {
		return StatusUnknown, false, err
	}
	if held {
		if !same {
			return StatusUnknown, false, ErrIDReused
		}
		return StatusCompleted, false, nil
	}
	q.pending[key] = msg
	return StatusPending, true, nil
}

// Status returns where the message the transport principal poster posted
// under id stands, signed or not (ADR 0026). Where poster's id names more
// than one message — one unsigned and one signed, or two issuers' — it
// answers pending when any of them is pending and completed only when
// none is, so a message still running is never reported completed. A
// message stays pending until its turn ends, which is after the turn
// commits it, so a message is never reported unknown between the two.
func (q *Queue) Status(ctx context.Context, poster, id string) (Status, error) {
	q.mu.Lock()
	pending := false
	for key, msg := range q.pending {
		if key.id == id && msg.poster == poster {
			pending = true
			break
		}
	}
	q.mu.Unlock()
	if pending {
		return StatusPending, nil
	}
	if q.runtime.Posted == nil {
		return StatusUnknown, nil
	}
	held, err := q.runtime.Posted(ctx, poster, id)
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
	return q.runtime.Committed(ctx, key.issuer, key.sender, key.id, content)
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

// Draining reports whether Drain has begun.
func (q *Queue) Draining() bool {
	select {
	case <-q.draining:
		return true
	default:
		return false
	}
}

// Drain stops admitting messages, lets the turn in flight run to
// completion with its durable writes, starts no queued turn, and then
// ends the monitoring stream. Queued messages stay uncommitted. The
// runtime stays open, so the caller decides what closes before it.
// Drain waits without a bound and is idempotent.
func (q *Queue) Drain() {
	q.drainOnce.Do(func() {
		close(q.draining)
		<-q.done
		q.cancel()
		q.broker.Close()
	})
}

// Close drains the queue and then releases the runtime, which closes
// memory and, last, its writer lock. Close is idempotent.
func (q *Queue) Close() error {
	q.Drain()
	var err error
	q.closeOnce.Do(func() {
		err = q.runtime.Close(context.Background())
	})
	return err
}
