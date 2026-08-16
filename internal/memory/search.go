package memory

import (
	"context"
	"fmt"
	"sort"
)

// Search implements the search operation: hybrid FTS5 + cosine ranking
// over the active index, access recorded on the results.
func (s *service) Search(ctx context.Context, query string, limit int) ([]ScoredMemory, error) {
	results, err := s.search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	if err := s.recordAccess(ctx, results); err != nil {
		return nil, err
	}
	return results, nil
}

// Similar implements the write-time coherence lookup. Unlike Search it
// does not record access: the results are surfaced for judgment, not
// injected into context.
func (s *service) Similar(ctx context.Context, content string, limit int) ([]ScoredMemory, error) {
	return s.search(ctx, content, limit)
}

// Recall implements the agent loop's recall step: hybrid search on the
// task statement, trimmed to the token budget, access recorded on the
// memories that made the cut.
//
// The budget is the only bound (ADR 0006 §Decision step 2: "a variable
// number of raw entries within a token budget"), so the candidates come
// from the ranking core rather than from the row-limited search: a row
// limit on top would decide the count before the budget could.
func (s *service) Recall(ctx context.Context, task string, tokenBudget int) ([]ScoredMemory, error) {
	if tokenBudget <= 0 {
		tokenBudget = DefaultRecallTokenBudget
	}
	// TEMPORARY: reintroduce the row bound, to prove the test catches it.
	candidates, err := s.search(ctx, task, 0)
	if err != nil {
		return nil, err
	}
	var (
		results []ScoredMemory
		used    int
	)
	for _, c := range candidates {
		cost := estimateTokens(c.Content)
		if len(results) > 0 && used+cost > tokenBudget {
			break
		}
		results = append(results, c)
		used += cost
	}
	if err := s.recordAccess(ctx, results); err != nil {
		return nil, err
	}
	return results, nil
}

// search returns the top limit of the ranked active index, defaulting to
// DefaultSearchLimit. It is the row-bounded form the Search and Similar
// operations take; Recall bounds the same ranking by tokens instead.
func (s *service) search(ctx context.Context, query string, limit int) ([]ScoredMemory, error) {
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	scored, err := s.rank(ctx, query)
	if err != nil {
		return nil, err
	}
	return scored[:min(limit, len(scored))], nil
}

// rank is the shared ranking core: brute-force cosine over stored
// embeddings plus FTS5 keyword match, combined by the one scoring
// function in ranking.go, in descending score order and unbounded. Each
// caller applies its own single bound.
func (s *service) rank(ctx context.Context, query string) ([]ScoredMemory, error) {
	emb, err := s.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	active, err := s.store.Active(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active: %w", err)
	}
	if len(active) == 0 {
		return nil, nil
	}
	ranks, err := s.store.FTSRanks(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("fts: %w", err)
	}
	ftsNorm := normalizeRanks(ranks)
	now := s.clock()
	scored := make([]ScoredMemory, 0, len(active))
	for _, m := range active {
		rel := relevance(cosine(emb, m.Embedding), ftsNorm[m.ID])
		scored = append(scored, ScoredMemory{Memory: m, Score: score(m, rel, now)})
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
	return scored, nil
}

// normalizeRanks converts raw FTS5 bm25 ranks (negative, smaller is
// better) into [0,1] scores relative to the best match. Rows without a
// keyword match are absent and score 0.
func normalizeRanks(ranks map[int64]float64) map[int64]float64 {
	var best float64
	for _, r := range ranks {
		best = max(best, -r)
	}
	out := make(map[int64]float64, len(ranks))
	if best == 0 {
		return out
	}
	for id, r := range ranks {
		out[id] = -r / best
	}
	return out
}
