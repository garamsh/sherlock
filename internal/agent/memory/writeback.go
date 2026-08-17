package memory

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/memory"
)

// coherenceNoteBudget bounds how many similar memories one writeback
// surfaces for judgment (ADR 0006 step 5); small like the recall budget,
// so the note never grows a turn unboundedly.
const coherenceNoteBudget = 5

// Writeback adapts the agent loop's memory writeback (ADR 0006 step 4) to
// the memory service. Every write goes through the service so the archive
// invariant (nothing is hard-deleted) stays owned by the memory package.
type Writeback struct {
	// Svc stores each write and answers the coherence lookup.
	Svc memory.Service
}

// Apply implements agent.WritebackSink: it stores each write, then
// retrieves the most similar existing memories for the coherence step
// (ADR 0006 step 5) — excluding the just-written versions — bounded by
// coherenceNoteBudget. An empty writeback retrieves nothing.
//
// The store still holds sentences, so each addressed write is rendered
// into one; #336 step 2 replaces the store with one that holds the
// address itself. Until it does, the declared operation reaches the
// store only as salience: the store infers no identity from what the
// write declared, and an insert retires nothing (ADR 0018), so a
// replacement coexists with what it replaces.
func (w Writeback) Apply(ctx context.Context, ops []agent.WritebackOp) ([]agent.RecalledMemory, error) {
	var notes []agent.RecalledMemory
	seen := make(map[int64]struct{}, len(ops))
	for _, op := range ops {
		sentence := sentenceOf(op)
		res, err := w.Svc.Store(ctx, memory.StoreInput{
			Content:         sentence,
			Type:            memory.TypeSemantic,
			ImportanceScore: salienceOf(op.Operation),
			Source:          "agent",
		})
		if err != nil {
			return nil, fmt.Errorf("writeback %s: %w", op.Operation, err)
		}
		if len(notes) >= coherenceNoteBudget {
			continue
		}
		matches, err := w.Svc.Similar(ctx, sentence, coherenceNoteBudget)
		if err != nil {
			return nil, fmt.Errorf("coherence lookup: %w", err)
		}
		for _, m := range matches {
			if m.ID == res.ID {
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

// salienceOf derives the stored salience from the operation the write
// states (ADR 0006): a replacement is a correction and ranks highest,
// everything else is a fact added beside what is held.
func salienceOf(op agent.MemoryOperation) float64 {
	if op == agent.OpReplace {
		return memory.SalienceUpdate
	}
	return memory.SalienceAddSemantic
}

// sentenceOf renders an addressed write as the sentence the shipped
// store holds: the subject, the question the value answers with whatever
// restriction it carries, and the value.
func sentenceOf(op agent.WritebackOp) string {
	question := op.Address.Question
	if op.Address.Restriction != "" {
		question += " (" + op.Address.Restriction + ")"
	}
	return fmt.Sprintf("%s — %s %s", op.Address.Subject, question, op.Value)
}
