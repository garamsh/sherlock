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

// StoreResult reports the new memory, the active predecessor it
// superseded (0 when none), and any entries evicted to keep the active
// index within capacity.
type StoreResult struct {
	ID           int64
	SupersededID int64
	EvictedIDs   []int64
}

// Store implements the store operation, with supersession on insert
// (ADR 0006): when the new memory's most similar active predecessor has
// an embedding cosine of at least SupersessionSimilarity, the new row
// takes over and the predecessor archives with superseded_by/valid_to
// set — the newest version of a fact is the version that lives.
func (s *service) Store(ctx context.Context, in StoreInput) (StoreResult, error) {
	if err := validateNew(in.Content, in.Type, in.ImportanceScore); err != nil {
		return StoreResult{}, err
	}
	emb, err := s.embedder.Embed(ctx, in.Content)
	if err != nil {
		return StoreResult{}, fmt.Errorf("embed: %w", err)
	}
	matches, err := s.Similar(ctx, in.Content, 1)
	if err != nil {
		return StoreResult{}, fmt.Errorf("supersession lookup: %w", err)
	}
	var superseded int64
	if len(matches) > 0 && cosine(emb, matches[0].Embedding) >= SupersessionSimilarity {
		superseded = matches[0].ID
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
	if superseded > 0 {
		if err := s.store.Archive(ctx, superseded, id, now); err != nil {
			return StoreResult{}, fmt.Errorf("archive superseded %d: %w", superseded, err)
		}
	}
	evicted, err := s.enforceCapacity(ctx)
	if err != nil {
		return StoreResult{}, err
	}
	return StoreResult{ID: id, SupersededID: superseded, EvictedIDs: evicted}, nil
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
