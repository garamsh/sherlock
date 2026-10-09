package memory

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/tool"
)

// Recall is the read step (ADR 0019, #336 step 3): it turns the turn's
// task into an address and reads the store at it.
//
// The store does no finding — asked in words an addressed store answers
// nothing at all — so this sits in front of it and is the only part of
// memory that reads language on the way in.
type Recall struct {
	// Svc answers the two addressed reads.
	Svc Service
	// Model resolves the question into an address. It is the session's
	// own model: nothing here needs a different one, and a second
	// provider section would be the shape #582 deleted.
	Model agent.Model
	// Now supplies the time memory ages are measured against; nil uses
	// time.Now.
	Now func() time.Time
}

// Recall implements agent.Recaller. The order is fixed and carries no
// threshold: resolve, read the address, and read the subject only when
// the address held nothing.
//
// The widening fires on an empty address rather than on the model's
// confidence. Emptiness is a fact the store reports for one indexed read
// and no model call, where a confidence would need a constant nothing
// calibrates — the shape ADR 0018 removed from this path rather than
// retuned.
func (r Recall) Recall(ctx context.Context, task string) (agent.Recalled, error) {
	address, err := r.resolve(ctx, task)
	if err != nil {
		return agent.Recalled{}, err
	}
	if !address.Placed() {
		return agent.Recalled{Outcome: agent.RecallUnplaced}, nil
	}

	if address.Question != "" {
		held, err := r.Svc.At(ctx, Address{
			Subject:  address.Subject,
			Question: address.Question,
		})
		if err != nil {
			return agent.Recalled{}, fmt.Errorf("read address: %w", err)
		}
		if len(held) > 0 {
			return agent.Recalled{Memories: r.render(held), Outcome: agent.RecallAddressed}, nil
		}
	}

	// Either the step placed a subject alone, or the address it placed
	// held nothing. Both are answered from every address under the
	// subject, which contains the answer whenever the subject is right —
	// the direction a read errs in (ADR 0019).
	under, err := r.Svc.UnderSubject(ctx, address.Subject)
	if err != nil {
		return agent.Recalled{}, fmt.Errorf("read subject: %w", err)
	}
	if len(under) == 0 {
		return agent.Recalled{Outcome: agent.RecallEmpty}, nil
	}
	return agent.Recalled{Memories: r.render(under), Outcome: agent.RecallWidened}, nil
}

// resolve is the one model call the read step makes. A response that
// carries no reserved call places nothing, which is a stated outcome:
// the step reports that it could not place the question rather than
// reading somewhere it guessed.
func (r Recall) resolve(ctx context.Context, task string) (agent.ReadAddress, error) {
	resp, err := r.Model.Chat(ctx, []agent.Message{
		{Role: agent.RoleSystem, Content: agent.ReadInstruction},
		{Role: agent.RoleUser, Content: task},
	}, []tool.Descriptor{agent.MemoryReadDescriptor()})
	if err != nil {
		return agent.ReadAddress{}, fmt.Errorf("resolve address: %w", err)
	}
	return agent.ParseReadAddress(resp), nil
}

// render turns held memories into what the model reads: the address and
// the value, because an addressed store holds nothing else and a value
// alone would not say what question it answers.
func (r Recall) render(held []Memory) []agent.RecalledMemory {
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	at := now()
	out := make([]agent.RecalledMemory, 0, len(held))
	for _, m := range held {
		out = append(out, agent.RecalledMemory{
			ID:      strconv.FormatInt(m.ID, 10),
			Content: line(m),
			Age:     age(at, m.Timestamp),
		})
	}
	return out
}

// line renders one held memory as the address it answers and the value
// held there.
func line(m Memory) string {
	question := m.Address.Question
	if m.Address.Restriction != "" {
		question += " (" + m.Address.Restriction + ")"
	}
	return fmt.Sprintf("%s — %s: %s", m.Address.Subject, question, m.Value)
}

// age renders how long ago a memory was stored, coarsened to whole
// minutes so the model reads durations like "3h0m0s" rather than
// sub-minute noise.
func age(now, stored time.Time) string {
	d := now.Sub(stored)
	if d < 0 {
		d = 0
	}
	return d.Round(time.Minute).String()
}
