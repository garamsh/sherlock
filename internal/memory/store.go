package memory

import (
	"context"
	"fmt"
	"sort"
)

// StoreInput is one addressed write (ADR 0018): where the fact goes, what
// it says, and what it does to the values already there.
type StoreInput struct {
	Address   Address
	Value     string
	Operation Operation
	Source    string
}

// StoreResult reports the written memory, the values a replacement
// superseded at its address, and any entries evicted to keep the active
// index within capacity. SupersededIDs is empty for a join, and empty for
// a replacement onto an address holding nothing.
//
// The write and the supersessions land together or not at all, so a
// failure never leaves a value and its correction both active at one
// address — which is the stale answer ADR 0018 exists to remove, and it
// would be nobody's to repair.
type StoreResult struct {
	ID            int64
	SupersededIDs []int64
	EvictedIDs    []int64
}

// Store implements the store operation: the value lands at its address,
// and the operation the write states decides what happens to the values
// already held there (ADR 0018).
//
// Identity is settled by the address. Nothing here compares the new value
// with what is held, because no threshold separates a correction from two
// facts that must both survive — on the shipped embedder the correction
// family's median cosine sits below the coexisting family's minimum
// (docs/experiments/memory-retrieval/). A replacement replaces because
// the write said so, and a join joins for the same reason.
//
// A replacement supersedes only the values at the address written. An
// address that restricts the same question is a different address holding
// the answer to a different question, so a narrowing statement never
// destroys the broader one (ADR 0018 §The address names the question).
//
// The insert and the supersessions it causes are one transaction. The
// address is read first to learn what is there; a value written between
// that read and the write is superseded by ID or not at all, never half.
func (s *service) Store(ctx context.Context, in StoreInput) (StoreResult, error) {
	if err := validate(in); err != nil {
		return StoreResult{}, err
	}
	var supersede []Memory
	if in.Operation == OpReplace {
		held, err := s.store.At(ctx, in.Address)
		if err != nil {
			return StoreResult{}, fmt.Errorf("read address: %w", err)
		}
		supersede = held
	}
	now := s.clock()
	id, superseded, err := s.store.Write(ctx, Memory{
		Address:         in.Address,
		Value:           in.Value,
		ImportanceScore: salienceOf(in.Operation),
		Timestamp:       now,
		Source:          in.Source,
	}, ids(supersede), now)
	if err != nil {
		return StoreResult{}, fmt.Errorf("write %s: %w", in.Operation, err)
	}
	evicted, err := s.enforceCapacity(ctx)
	if err != nil {
		return StoreResult{}, err
	}
	return StoreResult{ID: id, SupersededIDs: superseded, EvictedIDs: evicted}, nil
}

// ids is the IDs of the memories a replacement supersedes.
func ids(held []Memory) []int64 {
	out := make([]int64, 0, len(held))
	for _, m := range held {
		out = append(out, m.ID)
	}
	return out
}

// salienceOf derives the stored salience from the operation the write
// states (ADR 0006): a replacement is a correction and ranks highest, a
// value joining what is held ranks high. Salience is platform-derived
// here rather than in the caller, because the operation is what decides
// it and this is where the operation arrives.
func salienceOf(op Operation) float64 {
	if op == OpReplace {
		return SalienceReplace
	}
	return SalienceJoin
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
	// Stable, and the ID breaks a tie. Salience takes two values and a
	// timestamp is stored to the second, so equal scores are the common
	// case rather than the rare one, and an unspecified order among them
	// would make which memory is evicted depend on the sort.
	now := s.clock()
	sort.SliceStable(active, func(i, j int) bool {
		si, sj := score(active[i], now), score(active[j], now)
		if si != sj {
			return si < sj
		}
		return active[i].ID < active[j].ID
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
