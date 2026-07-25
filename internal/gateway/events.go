package gateway

import (
	"context"
	"sync"

	"github.com/garamsh/gagent/internal/agent"
)

// SubscriberBuffer bounds how many undelivered events one SSE subscriber
// may accumulate before the broker starts dropping events for it.
const SubscriberBuffer = 32

// Broker is one environment's monitoring stream (ADR 0009): it receives
// loop events as the session's agent.EventSink and fans them out to
// subscribers. Publishing never blocks the run — a subscriber with a full
// buffer drops events — and zero subscribers is the normal case. Broker
// is safe for concurrent use.
type Broker struct {
	mu     sync.Mutex
	subs   map[chan agent.Event]struct{}
	closed bool
}

// NewBroker returns an empty broker.
func NewBroker() *Broker {
	return &Broker{subs: make(map[chan agent.Event]struct{})}
}

// Emit implements agent.EventSink. Delivery is best-effort: events go to
// every subscriber with buffer room and are dropped for the rest.
func (b *Broker) Emit(_ context.Context, e agent.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// Subscribe registers a new event consumer. The returned cancel function
// unregisters it; cancel is idempotent. Subscribing to a closed broker
// yields an already-closed channel.
func (b *Broker) Subscribe() (<-chan agent.Event, func()) {
	ch := make(chan agent.Event, SubscriberBuffer)
	b.mu.Lock()
	if b.closed {
		close(ch)
		b.mu.Unlock()
		return ch, func() {}
	}
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			// Close may have already ended the stream; only the
			// registration owner closes the channel.
			if _, ok := b.subs[ch]; ok {
				delete(b.subs, ch)
				close(ch)
			}
		})
	}
	return ch, cancel
}

// Close ends the stream: every subscriber channel is closed and future
// events are discarded.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for ch := range b.subs {
		close(ch)
	}
	b.subs = make(map[chan agent.Event]struct{})
}
