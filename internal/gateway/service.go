package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/garamsh/gagent/internal/agent"
)

// DefaultQueueCapacity bounds each environment's followup queue. Enqueue
// blocks when the queue is full, giving senders natural backpressure.
const DefaultQueueCapacity = 64

// RuntimeFactory builds one environment's runtime on first use. The
// events sink is the environment's monitoring stream and outlives any
// single runtime.
type RuntimeFactory interface {
	New(ctx context.Context, envID string, events agent.EventSink) (*Runtime, error)
}

// Router is the gateway's core: it routes inbound messages to per
// environment queues so each environment runs one turn at a time
// (ADR 0009). Environments are created on first use. Router is safe for
// concurrent use; Close shuts every environment down.
type Router struct {
	factory RuntimeFactory
	logger  *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.Mutex
	envs    map[string]*envQueue
	brokers map[string]*Broker
	closed  bool
}

// NewRouter returns a router that builds environments through factory.
// A nil logger discards log output.
func NewRouter(factory RuntimeFactory, logger *slog.Logger) *Router {
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Router{
		factory: factory,
		logger:  logger,
		ctx:     ctx,
		cancel:  cancel,
		envs:    make(map[string]*envQueue),
		brokers: make(map[string]*Broker),
	}
}

// Enqueue assigns the message an ID and queues it for the environment,
// creating the environment on first use. It returns the assigned ID once
// the message is queued; there is no synchronous agent response. A full
// queue blocks the caller (honoring ctx) until a run drains a slot.
func (r *Router) Enqueue(ctx context.Context, envID, content string) (string, error) {
	q, err := r.env(envID)
	if err != nil {
		return "", err
	}
	id, err := newID("msg")
	if err != nil {
		return "", err
	}
	msg := Inbound{ID: id, EnvID: envID, Content: content, QueuedAt: time.Now()}
	select {
	case q.inbox <- msg:
		r.logger.Info("message enqueued",
			"op", "enqueue", "env_id", envID, "message_id", id)
		return id, nil
	case <-ctx.Done():
		return "", ctx.Err()
	case <-r.ctx.Done():
		return "", ErrRouterClosed
	}
}

// Events returns the environment's monitoring stream, creating it on
// first use. Subscribing never creates the environment itself.
func (r *Router) Events(envID string) *Broker {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lockedBroker(envID)
}

// Close stops accepting messages, cancels in-flight runs, waits for the
// runners to exit, and releases every environment runtime and stream.
func (r *Router) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.cancel()
	envs := make([]*envQueue, 0, len(r.envs))
	for _, q := range r.envs {
		envs = append(envs, q)
	}
	brokers := make([]*Broker, 0, len(r.brokers))
	for _, b := range r.brokers {
		brokers = append(brokers, b)
	}
	r.mu.Unlock()

	r.wg.Wait()
	var errs []error
	for _, q := range envs {
		if err := q.runtime.Close(context.Background()); err != nil {
			errs = append(errs, fmt.Errorf("close env %q: %w", q.envID, err))
		}
	}
	for _, b := range brokers {
		b.Close()
	}
	return errors.Join(errs...)
}

// env returns the environment's queue, creating the runtime and runner on
// first use. Creation is synchronous so factory errors (e.g. an invalid
// env ID) surface to the sender instead of after the ack.
func (r *Router) env(envID string) (*envQueue, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, ErrRouterClosed
	}
	if q, ok := r.envs[envID]; ok {
		return q, nil
	}
	rt, err := r.factory.New(r.ctx, envID, r.lockedBroker(envID))
	if err != nil {
		return nil, fmt.Errorf("create environment %q: %w", envID, err)
	}
	q := &envQueue{
		envID:   envID,
		runtime: rt,
		inbox:   make(chan Inbound, DefaultQueueCapacity),
	}
	r.envs[envID] = q
	r.wg.Add(1)
	go r.run(q)
	return q, nil
}

// lockedBroker returns the env's broker, creating it on first use. The
// caller must hold r.mu.
func (r *Router) lockedBroker(envID string) *Broker {
	if b, ok := r.brokers[envID]; ok {
		return b
	}
	b := NewBroker()
	r.brokers[envID] = b
	return b
}
