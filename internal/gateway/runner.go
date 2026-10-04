package gateway

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/garamsh/sherlock/internal/agent"
)

// MaxDrainBatch bounds how many queued messages one turn takes (ADR 0015);
// overflow stays queued for the next turn.
const MaxDrainBatch = 8

// run is the agent's runner loop. A turn drains the queue: the first
// message plus everything waiting behind it, up to MaxDrainBatch,
// rendered as one turn input (ADR 0015). A failed turn is logged and the
// queue continues with the next drain. Once draining begins it starts no
// further turn; the one in flight runs to completion.
func (q *Queue) run(events *batchEvents) {
	defer close(q.done)
	logger := q.logger.With("agent_id", q.agentID)
	for {
		select {
		case <-q.draining:
			return
		case msg := <-q.inbox:
			// select picks at random when both are ready, so a message
			// taken as the drain began starts no turn.
			if q.Draining() {
				return
			}
			batch := drain(q.inbox, msg)
			ids := messageIDs(batch)
			events.setBatch(ids)
			_, err := q.runtime.Session.Run(agent.WithInboundSources(q.ctx, sourceBatch(batch)), renderBatch(batch))
			events.setBatch(nil)
			// The turn has committed its sources or never will.
			q.forget(messageKeys(batch))
			if err != nil {
				logger.Error("run failed",
					"op", "run", "message_ids", ids, "error", err)
				continue
			}
			logger.Info("run finished", "op", "run", "message_ids", ids)
			// The runtime's after-turn pass runs after the completed
			// turn, never mid-turn; a failure leaves the store
			// consistent and only logs. A drain skips it: the pass is
			// idempotent, so the next start picks up what it left.
			if q.runtime.Retain != nil && !q.Draining() {
				if err := q.runtime.Retain(q.ctx); err != nil {
					logger.Warn("after-turn pass failed",
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

func messageKeys(batch []Inbound) []messageKey {
	keys := make([]messageKey, len(batch))
	for i, m := range batch {
		keys[i] = messageKey{sender: m.Sender, id: m.ID}
	}
	return keys
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

func sourceBatch(batch []Inbound) []agent.SourceMessage {
	out := make([]agent.SourceMessage, 0, len(batch))
	for _, m := range batch {
		out = append(out, agent.SourceMessage{ExternalID: m.ID, Sender: m.Sender, Role: agent.RoleUser, Content: m.Content})
	}
	return out
}
