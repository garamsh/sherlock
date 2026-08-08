package gateway

import (
	"context"
	"strconv"
	"time"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/memory"
)

// Recall adapts the memory service to the agent loop's recall hook
// (ADR 0008 step 2): the task statement is the hybrid-search query and
// results are trimmed to the token budget.
type Recall struct {
	// Svc is the agent's memory service.
	Svc memory.Service
	// Budget bounds the tokens injected per turn; zero uses the memory
	// package default.
	Budget int
	// Now supplies the time used to compute memory ages; nil uses
	// time.Now.
	Now func() time.Time
}

// Recall implements agent.Recaller.
func (r Recall) Recall(ctx context.Context, task string) ([]agent.RecalledMemory, error) {
	results, err := r.Svc.Recall(ctx, task, r.Budget)
	if err != nil {
		return nil, err
	}
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	out := make([]agent.RecalledMemory, 0, len(results))
	for _, m := range results {
		out = append(out, agent.RecalledMemory{
			ID:      strconv.FormatInt(m.ID, 10),
			Content: m.Content,
			Age:     age(now(), m.Timestamp),
		})
	}
	return out, nil
}

// age renders how long ago the memory was stored, coarsened to whole
// minutes so the model reads durations like "3h0m0s", never sub-minute
// noise.
func age(now, stored time.Time) string {
	d := now.Sub(stored)
	if d < 0 {
		d = 0
	}
	return d.Round(time.Minute).String()
}
