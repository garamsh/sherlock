package gateway

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/garamsh/gagent/internal/agent"
)

// MaxDrainBatch bounds how many queued messages one turn takes (ADR 0020);
// overflow stays queued for the next turn.
const MaxDrainBatch = 8

// envQueue serializes one environment's runs: the inbox holds accepted
// followups in FIFO order and exactly one runner goroutine drains it, so
// there is never more than one active run per environment (ADR 0009).
type envQueue struct {
	agentID string
	runtime *Runtime
	inbox   chan Inbound
	events  *batchEvents
}

// run is the environment's runner loop. A turn drains the queue: the
// first message plus everything waiting behind it, up to MaxDrainBatch,
// rendered as one turn input (ADR 0020). A failed turn is logged and the
// queue continues with the next drain.
func (r *Router) run(q *envQueue) {
	defer r.wg.Done()
	logger := r.logger.With("agent_id", q.agentID)
	for {
		select {
		case <-r.ctx.Done():
			return
		case msg := <-q.inbox:
			batch := drain(q.inbox, msg)
			ids := messageIDs(batch)
			q.events.setBatch(ids)
			_, err := q.runtime.Session.Run(r.ctx, renderBatch(batch))
			q.events.setBatch(nil)
			if err != nil {
				logger.Error("run failed",
					"op", "run", "message_ids", ids, "error", err)
				continue
			}
			logger.Info("run finished", "op", "run", "message_ids", ids)
		}
	}
}

// drain returns msg plus every message already waiting in the inbox,
// bounded by MaxDrainBatch; overflow stays queued for the next turn.
func drain(inbox <-chan Inbound, msg Inbound) []Inbound {
	batch := []Inbound{msg}
	for len(batch) < MaxDrainBatch {
		select {
		case m := <-inbox:
			batch = append(batch, m)
		default:
			return batch
		}
	}
	return batch
}

// renderBatch renders one drained batch as the turn's input: one line
// per message carrying its ID, sender, and content (ADR 0020). A message
// without a sender renders as "unknown".
func renderBatch(batch []Inbound) string {
	var b strings.Builder
	for i, m := range batch {
		sender := m.Sender
		if sender == "" {
			sender = "unknown"
		}
		fmt.Fprintf(&b, "[%s] %s: %s", m.ID, sender, m.Content)
		if i < len(batch)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func messageIDs(batch []Inbound) []string {
	ids := make([]string, len(batch))
	for i, m := range batch {
		ids[i] = m.ID
	}
	return ids
}

// batchEvents is the environment's event sink: it stamps terminal events
// with the message IDs of the batch the turn drained (ADR 0020) and
// forwards everything to the monitoring stream.
type batchEvents struct {
	inner agent.EventSink

	mu  sync.Mutex
	ids []string
}

// setBatch records the message IDs of the batch whose turn is running;
// nil clears it once the turn's terminal event has been emitted.
func (s *batchEvents) setBatch(ids []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ids = ids
}

// Emit implements agent.EventSink.
func (s *batchEvents) Emit(ctx context.Context, e agent.Event) {
	if e.Kind == agent.EventTurnFinished || e.Kind == agent.EventTurnFailed {
		s.mu.Lock()
		e.MessageIDs = slices.Clone(s.ids)
		s.mu.Unlock()
	}
	s.inner.Emit(ctx, e)
}
