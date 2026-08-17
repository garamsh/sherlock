package gateway

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/garamsh/gagent/internal/agent"
)

// MaxDrainBatch bounds how many queued messages one turn takes (ADR 0015);
// overflow stays queued for the next turn.
const MaxDrainBatch = 8

// run is the agent's runner loop. A turn drains the queue: the first
// message plus everything waiting behind it, up to MaxDrainBatch,
// rendered as one turn input (ADR 0015). A failed turn is logged and the
// queue continues with the next drain.
func (q *Queue) run(events *batchEvents) {
	defer close(q.done)
	logger := q.logger.With("agent_id", q.agentID)
	for {
		select {
		case <-q.ctx.Done():
			return
		case msg := <-q.inbox:
			batch := drain(q.inbox, msg)
			ids := messageIDs(batch)
			events.setBatch(ids)
			_, err := q.runtime.Session.Run(q.ctx, renderBatch(batch))
			events.setBatch(nil)
			if err != nil {
				logger.Error("run failed",
					"op", "run", "message_ids", ids, "error", err)
				continue
			}
			logger.Info("run finished", "op", "run", "message_ids", ids)
			// Threshold consolidation (ADR 0011) and tiered retention
			// (ADR 0006) evaluate after the completed turn, never
			// mid-turn; a failure leaves the store consistent and only
			// logs.
			if q.runtime.ConsolidateIfDue != nil {
				if err := q.runtime.ConsolidateIfDue(q.ctx); err != nil {
					logger.Warn("consolidation failed",
						"op", "consolidate", "error", err)
				}
			}
			if q.runtime.Retain != nil {
				if err := q.runtime.Retain(q.ctx); err != nil {
					logger.Warn("retention failed",
						"op", "retain", "error", err)
				}
			}
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
// per message carrying its ID, sender, and content (ADR 0015). A message
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

// batchEvents is the agent's event sink: it stamps terminal events with
// the message IDs of the batch the turn drained (ADR 0015) and forwards
// everything to the monitoring stream.
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
