package memory

import (
	"context"
	"fmt"
)

// Retain implements the tiered retention pass (ADR 0006): memories with
// salience at or above RetentionKeepSalience are kept indefinitely,
// memories below RetentionDropSalience drop to the archive at the next
// pass, and the tier between is retained for RetentionWindow from its
// timestamp. Drops move to the archive; the platform never hard-deletes.
// The gateway evaluates Retain after a completed turn.
func (s *service) Retain(ctx context.Context) ([]int64, error) {
	active, err := s.store.Active(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active: %w", err)
	}
	now := s.clock()
	var archived []int64
	for _, m := range active {
		if m.ImportanceScore >= RetentionKeepSalience {
			continue
		}
		if m.ImportanceScore >= RetentionDropSalience &&
			now.Sub(m.Timestamp) <= RetentionWindow {
			continue
		}
		if err := s.store.Archive(ctx, m.ID, 0, now); err != nil {
			return nil, fmt.Errorf("archive %d: %w", m.ID, err)
		}
		archived = append(archived, m.ID)
	}
	return archived, nil
}
