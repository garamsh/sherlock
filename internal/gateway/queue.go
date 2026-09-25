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
}

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
	}
	go q.run(events)
	return q, nil
}

// AgentID returns the ID of the agent this process serves.
func (q *Queue) AgentID() string { return q.agentID }

// Enqueue assigns the message an ID and queues it for the agent. It
// returns the assigned ID once the message is queued; there is no
// synchronous agent response. A full queue blocks the caller (honoring
// ctx) until a run drains a slot. The sender is recorded on the envelope
// so the agent can address replies per message (ADR 0015); an empty
// sender means the channel provided none. Nothing is written to memory
// at accept time: the writeback is the single write path
// (ADR 0006, ADR 0011).
func (q *Queue) Enqueue(ctx context.Context, sender, content string) (string, error) {
	select {
	case <-q.closed:
		return "", ErrQueueClosed
	default:
	}
	id, err := newID("msg")
	if err != nil {
		return "", err
	}
	msg := Inbound{ID: id, Sender: sender, Content: content, QueuedAt: time.Now()}
	select {
	case q.inbox <- msg:
		q.logger.Info("message enqueued",
			"op", "enqueue", "agent_id", q.agentID, "message_id", id)
		return id, nil
	case <-ctx.Done():
		return "", ctx.Err()
	case <-q.ctx.Done():
		return "", ErrQueueClosed
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
