package memory

import (
	"context"
	"fmt"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/memory"
)

// Writeback adapts the agent loop's memory writeback (ADR 0006 step 4) to
// the memory service. Every write goes through the service so the archive
// invariant (nothing is hard-deleted) stays owned by the memory package.
type Writeback struct {
	// Svc stores each write.
	Svc memory.Service
}

// Apply implements agent.WritebackSink: it stores each write at its
// address, with the operation the write states deciding what happens to
// the values already there (ADR 0018).
//
// It returns no coherence note, and the sink no longer has a shape in
// which it could. ADR 0006 step 5 surfaced the most similar existing
// memories for the model to judge, which is what a store that could not
// compute which memory a write was for needed; the address computes it
// and the stated operation says what to do about it. A turn now ends on
// this call, so there is also no later model call within it to read one.
func (w Writeback) Apply(ctx context.Context, ops []agent.WritebackOp) error {
	for _, op := range ops {
		_, err := w.Svc.Store(ctx, memory.StoreInput{
			Address: memory.Address{
				Subject:     op.Address.Subject,
				Question:    op.Address.Question,
				Restriction: op.Address.Restriction,
			},
			Value:     op.Value,
			Operation: operationOf(op.Operation),
			Source:    "agent",
		})
		if err != nil {
			return fmt.Errorf("writeback %s: %w", op.Operation, err)
		}
	}
	return nil
}

// operationOf carries the parsed operation across the domain boundary.
// Anything that is not a stated replacement is a join, which is the
// direction a writeback errs in (ADR 0018 §Where a writeback must err):
// a wrong join is repaired by the next correct replacement at that
// address, and a wrong replacement is repaired by nothing.
func operationOf(op agent.MemoryOperation) memory.Operation {
	if op == agent.OpReplace {
		return memory.OpReplace
	}
	return memory.OpJoin
}
