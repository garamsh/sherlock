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

// Writeback adapts the agent loop's memory writeback (ADR 0008 step 4) to
// the memory service. Deletes go through the store because the service
// exposes no per-ID delete; archiving keeps the platform rule that
// nothing is hard-deleted.
type Writeback struct {
	// Svc applies add and update operations.
	Svc memory.Service
	// Store archives deleted memories by ID.
	Store memory.Store
	// Now supplies the archive timestamp; nil uses time.Now.
	Now func() time.Time
}

// Apply implements agent.WritebackSink.
func (w Writeback) Apply(ctx context.Context, ops []agent.WritebackOp) error {
	for _, op := range ops {
		if err := w.applyOne(ctx, op); err != nil {
			return fmt.Errorf("writeback %s: %w", op.Kind, err)
		}
	}
	return nil
}

func (w Writeback) applyOne(ctx context.Context, op agent.WritebackOp) error {
	switch op.Kind {
	case agent.OpKeep:
		// Access was already recorded during recall; nothing to write.
		return nil
	case agent.OpAdd:
		typ := memory.Type(op.Type)
		if typ == "" {
			typ = memory.TypeEpisodic
		}
		_, err := w.Svc.Store(ctx, memory.StoreInput{
			Content:         op.Content,
			Type:            typ,
			ImportanceScore: DefaultAddImportance,
			Source:          "agent",
		})
		return err
	case agent.OpUpdate:
		id, err := parseMemoryID(op.ID)
		if err != nil {
			return err
		}
		_, err = w.Svc.Update(ctx, memory.UpdateInput{ID: id, Content: op.Content})
		return err
	case agent.OpDelete:
		id, err := parseMemoryID(op.ID)
		if err != nil {
			return err
		}
		now := time.Now
		if w.Now != nil {
			now = w.Now
		}
		return w.Store.Archive(ctx, id, 0, now())
	default:
		return fmt.Errorf("unknown writeback op %q", op.Kind)
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
