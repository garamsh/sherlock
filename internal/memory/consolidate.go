package memory

import (
	"context"
	"fmt"
)

// ConsolidationMinCluster is the smallest set of active episodic memories
// worth compressing into a semantic parent; below it Consolidate is a
// no-op. The threshold is a constant, not config: ADR 0011 makes
// consolidation platform-owned, and no caller needs to tune it.
const ConsolidationMinCluster = 2

// ConsolidationThreshold is the store-size trigger (ADR 0006, ADR 0011):
// when more than this many episodic memories are active, the platform
// consolidates after a completed turn, so a long-lived agent never
// accumulates unconsolidated episodic memory indefinitely. A constant,
// not config — no caller needs to tune it.
const ConsolidationThreshold = 100

// Consolidate is the platform-owned consolidation entry point (ADR 0011):
// when at least ConsolidationMinCluster episodic memories are active they
// compress into one semantic parent with the originals archived. The
// gateway calls it on shutdown; the after-turn trigger is
// ConsolidateIfDue.
func (s *service) Consolidate(ctx context.Context) (CompressResult, error) {
	active, err := s.store.Active(ctx)
	if err != nil {
		return CompressResult{}, fmt.Errorf("list active: %w", err)
	}
	var ids []int64
	for _, m := range active {
		if m.Type == TypeEpisodic {
			ids = append(ids, m.ID)
		}
	}
	if len(ids) < ConsolidationMinCluster {
		return CompressResult{}, nil
	}
	return s.Compress(ctx, ids)
}

// ConsolidateIfDue implements the threshold trigger: it runs Consolidate
// when the active episodic count has crossed ConsolidationThreshold and
// is a no-op below it. A failure leaves the store consistent — nothing
// is archived until Compress succeeds.
func (s *service) ConsolidateIfDue(ctx context.Context) (CompressResult, error) {
	active, err := s.store.Active(ctx)
	if err != nil {
		return CompressResult{}, fmt.Errorf("list active: %w", err)
	}
	episodic := 0
	for _, m := range active {
		if m.Type == TypeEpisodic {
			episodic++
		}
	}
	if episodic < ConsolidationThreshold {
		return CompressResult{}, nil
	}
	return s.Consolidate(ctx)
}
