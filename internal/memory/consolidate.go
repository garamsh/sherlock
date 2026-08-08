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

// Consolidate is the platform-owned consolidation trigger (ADR 0011): the
// gateway calls it on environment shutdown, and when at least
// ConsolidationMinCluster episodic memories are active they compress into
// one semantic parent with the originals archived.
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
