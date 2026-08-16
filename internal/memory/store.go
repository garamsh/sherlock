package memory

import (
	"context"
	"fmt"
	"sort"
)

// StoreInput carries the fields required to store a new memory.
type StoreInput struct {
	Content         string
	Type            Type
	ImportanceScore float64
	Source          string
}

// StoreResult reports the new memory and any entries evicted to keep
// the active index within capacity. SupersededID is always 0: an insert
// retires nothing (see Store). It stays because
// docs/experiments/memory-retrieval/'s harness reads it to report what
// the mechanism did while it existed.
type StoreResult struct {
	ID           int64
	SupersededID int64
	EvictedIDs   []int64
}

// Store implements the store operation. An insert adds; it never
// retires an existing memory.
//
// Identity is not inferred from similarity (ADR 0006 §Decision, ADR
// 0018): no threshold separates a correction from two facts that must
// both survive, because on the shipped embedder the correction family's
// median cosine sits below the coexisting family's minimum
// (docs/experiments/memory-retrieval/). Supersession on insert read
// that cosine anyway and retired the loser, which destroyed facts that
// answer different questions about one subject once writes carried
// their address in the content (#529).
//
// Until the addressed store lands (#336 step 2) the cost is that a
// correction coexists with the value it corrects rather than replacing
// it. That is the accumulating direction, which the next correct write
// repairs — ADR 0018 §Where a writeback must err prefers it to the
// destroying direction, which nothing repairs.
func (s *service) Store(ctx context.Context, in StoreInput) (StoreResult, error) {
	if err := validateNew(in.Content, in.Type, in.ImportanceScore); err != nil {
		return StoreResult{}, err
	}
	emb, err := s.embedder.Embed(ctx, in.Content)
	if err != nil {
		return StoreResult{}, fmt.Errorf("embed: %w", err)
	}
	now := s.clock()
	id, err := s.store.Insert(ctx, Memory{
		Content:         in.Content,
		Embedding:       emb,
		Type:            in.Type,
		ImportanceScore: in.ImportanceScore,
		Timestamp:       now,
		Source:          in.Source,
	})
	if err != nil {
		return StoreResult{}, fmt.Errorf("insert: %w", err)
	}
	evicted, err := s.enforceCapacity(ctx)
	if err != nil {
		return StoreResult{}, err
	}
	return StoreResult{ID: id, EvictedIDs: evicted}, nil
}

// enforceCapacity evicts lowest-score-first to the archive until the
// active index is within capacity.
func (s *service) enforceCapacity(ctx context.Context) ([]int64, error) {
	active, err := s.store.Active(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active: %w", err)
	}
	if len(active) <= s.capacity {
		return nil, nil
	}
	now := s.clock()
	sort.Slice(active, func(i, j int) bool {
		return score(active[i], 0, now) < score(active[j], 0, now)
	})
	evicted := make([]int64, 0, len(active)-s.capacity)
	for _, m := range active[:len(active)-s.capacity] {
		if err := s.store.Archive(ctx, m.ID, 0, now); err != nil {
			return nil, fmt.Errorf("evict %d: %w", m.ID, err)
		}
		evicted = append(evicted, m.ID)
	}
	return evicted, nil
}
