package gateway

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/memory"
)

// DefaultAddImportance is the importance score assigned to writeback
// additions, which carry no score of their own.
const DefaultAddImportance = 0.5

// coherenceNoteBudget bounds how many similar memories one writeback
// surfaces for judgment (ADR 0006 step 5); small like the recall budget,
// so the note never grows a turn unboundedly.
const coherenceNoteBudget = 5

// Writeback adapts the agent loop's memory writeback (ADR 0006 step 4) to
// the memory service. Deletes go through the service so the archive
// invariant (nothing is hard-deleted) stays owned by the memory package.
type Writeback struct {
	// Svc applies add, update, and delete operations.
	Svc memory.Service
}

// Apply implements agent.WritebackSink: it applies the ops, then for each
// add/update retrieves the most similar existing memories for the
// coherence step (ADR 0006 step 5) — excluding the just-written versions —
// bounded by coherenceNoteBudget. Keep/delete-only writebacks retrieve
// nothing.
func (w Writeback) Apply(ctx context.Context, ops []agent.WritebackOp) ([]agent.RecalledMemory, error) {
	var notes []agent.RecalledMemory
	seen := make(map[int64]struct{}, len(ops))
	for _, op := range ops {
		newID, err := w.applyOne(ctx, op)
		if err != nil {
			return nil, fmt.Errorf("writeback %s: %w", op.Kind, err)
		}
		if op.Kind != agent.OpAdd && op.Kind != agent.OpUpdate {
			continue
		}
		if len(notes) >= coherenceNoteBudget {
			continue
		}
		matches, err := w.Svc.Similar(ctx, op.Content, coherenceNoteBudget)
		if err != nil {
			return nil, fmt.Errorf("coherence lookup: %w", err)
		}
		for _, m := range matches {
			if m.ID == newID {
				continue
			}
			if _, dup := seen[m.ID]; dup {
				continue
			}
			seen[m.ID] = struct{}{}
			notes = append(notes, agent.RecalledMemory{
				ID:      strconv.FormatInt(m.ID, 10),
				Content: m.Content,
				Age:     age(time.Now(), m.Timestamp),
			})
			if len(notes) >= coherenceNoteBudget {
				break
			}
		}
	}
	return notes, nil
}

// applyOne applies one op and returns the ID of the memory version it
// wrote, or 0 for keep/delete.
func (w Writeback) applyOne(ctx context.Context, op agent.WritebackOp) (int64, error) {
	switch op.Kind {
	case agent.OpKeep:
		// Access was already recorded during recall; nothing to write.
		return 0, nil
	case agent.OpAdd:
		typ := memory.Type(op.Type)
		if typ == "" {
			typ = memory.TypeEpisodic
		}
		res, err := w.Svc.Store(ctx, memory.StoreInput{
			Content:         op.Content,
			Type:            typ,
			ImportanceScore: DefaultAddImportance,
			Source:          "agent",
		})
		return res.ID, err
	case agent.OpUpdate:
		id, err := parseMemoryID(op.ID)
		if err != nil {
			return 0, err
		}
		res, err := w.Svc.Update(ctx, memory.UpdateInput{ID: id, Content: op.Content})
		return res.ID, err
	case agent.OpDelete:
		id, err := parseMemoryID(op.ID)
		if err != nil {
			return 0, err
		}
		return 0, w.Svc.Delete(ctx, id)
	default:
		return 0, fmt.Errorf("unknown writeback op %q", op.Kind)
	}
}

// parseMemoryID converts the string IDs the model sees back to store IDs.
func parseMemoryID(id string) (int64, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid memory id %q: %w", id, err)
	}
	return n, nil
}
