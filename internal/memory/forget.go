package memory

import (
	"context"
	"fmt"
)

// Forget implements the forget operation: active memories scoring below
// the threshold move to the archive. The score is the same weighted
// function as ranking, without a query (relevance 0).
func (s *service) Forget(ctx context.Context, threshold float64) ([]int64, error) {
	if threshold <= 0 {
		threshold = DefaultForgetThreshold
	}
	active, err := s.store.Active(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active: %w", err)
	}
	now := s.clock()
	var archived []int64
	for _, m := range active {
		if score(m, 0, now) >= threshold {
			continue
		}
		if err := s.store.Archive(ctx, m.ID, 0, now); err != nil {
			return nil, fmt.Errorf("archive %d: %w", m.ID, err)
		}
		archived = append(archived, m.ID)
	}
	return archived, nil
}
