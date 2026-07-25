package memory

import (
	"context"
	"fmt"
	"time"
)

const (
	// DefaultCapacity caps the active (searchable) index.
	DefaultCapacity = 10_000
	// DefaultSearchLimit bounds search results when the caller passes no
	// positive limit.
	DefaultSearchLimit = 10
)

// Store hides the persistence engine (ADR 0003 escape hatch). It owns
// rows and indexes only; all scoring lives in the service so retrieval,
// eviction, and forget share one ranking function.
type Store interface {
	// Insert appends a new active memory and returns its ID.
	Insert(ctx context.Context, m Memory) (int64, error)
	// Get returns one active memory by ID, or ErrNotFound.
	Get(ctx context.Context, id int64) (Memory, error)
	// Active returns every active memory, including embeddings.
	Active(ctx context.Context) ([]Memory, error)
	// FTSRanks returns the raw FTS5 rank per row matching query; rows
	// with no keyword match are absent from the map.
	FTSRanks(ctx context.Context, query string) (map[int64]float64, error)
	// Archive moves an active memory to the archive table, recording
	// supersededBy (0 when not superseded) and validTo.
	Archive(ctx context.Context, id int64, supersededBy int64, validTo time.Time) error
	// RecordAccess increments access_count and sets last_accessed on the
	// given active memories.
	RecordAccess(ctx context.Context, ids []int64, at time.Time) error
	// Close releases the store.
	Close() error
}

// Embedder converts text to an embedding vector (ADR 0005).
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// Summarizer compresses a set of memory contents into one parent text.
type Summarizer interface {
	Summarize(ctx context.Context, contents []string) (string, error)
}

// Service is the memory tool's behavior contract: the five operations
// plus the runtime queries (Recall, Similar) the agent loop needs.
type Service interface {
	// Store embeds and inserts a new memory, evicting lowest-score-first
	// when the active index exceeds capacity.
	Store(ctx context.Context, in StoreInput) (StoreResult, error)
	// Search runs hybrid search and records access on the returned
	// memories. A non-positive limit falls back to DefaultSearchLimit.
	Search(ctx context.Context, query string, limit int) ([]ScoredMemory, error)
	// Update supersedes a memory: a new row is inserted and the old row
	// is archived with superseded_by/valid_to set. Never overwrites.
	Update(ctx context.Context, in UpdateInput) (UpdateResult, error)
	// Compress summarizes a set of episodic memories into one semantic
	// parent and archives the originals.
	Compress(ctx context.Context, ids []int64) (CompressResult, error)
	// Forget archives active memories whose lifecycle score falls below
	// threshold. A non-positive threshold uses DefaultForgetThreshold.
	Forget(ctx context.Context, threshold float64) ([]int64, error)
	// Recall is the agent loop's search entry point: hybrid search on the
	// task statement, trimmed to a token budget, access recorded.
	Recall(ctx context.Context, task string, tokenBudget int) ([]ScoredMemory, error)
	// Similar returns the most similar active memories for write-time
	// coherence. It does not record access.
	Similar(ctx context.Context, content string, limit int) ([]ScoredMemory, error)
	// Close releases the underlying store.
	Close() error
}

type service struct {
	store      Store
	embedder   Embedder
	summarizer Summarizer
	clock      func() time.Time
	capacity   int
}

// Option customizes a service; defaults come from the package constants.
type Option func(*service)

// WithClock substitutes the time source (tests).
func WithClock(clock func() time.Time) Option {
	return func(s *service) { s.clock = clock }
}

// WithCapacity overrides DefaultCapacity.
func WithCapacity(capacity int) Option {
	return func(s *service) { s.capacity = capacity }
}

// NewService returns the memory service over the given store.
func NewService(store Store, embedder Embedder, summarizer Summarizer, opts ...Option) Service {
	s := &service{
		store:      store,
		embedder:   embedder,
		summarizer: summarizer,
		clock:      time.Now,
		capacity:   DefaultCapacity,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Close closes the underlying store.
func (s *service) Close() error { return s.store.Close() }

// validateNew rejects values that would corrupt the store. Agent input is
// already schema-validated at the tool boundary; this guards the module
// boundary for direct runtime callers.
func validateNew(content string, typ Type, importance float64) error {
	if content == "" {
		return fmt.Errorf("%w: empty content", ErrInvalidInput)
	}
	if typ != TypeEpisodic && typ != TypeSemantic {
		return fmt.Errorf("%w: unknown type %q", ErrInvalidInput, typ)
	}
	if importance < 0 || importance > 1 {
		return fmt.Errorf("%w: importance_score %v outside [0,1]", ErrInvalidInput, importance)
	}
	return nil
}

// recordAccess marks returned memories as accessed (ADR 0008, Act step).
func (s *service) recordAccess(ctx context.Context, results []ScoredMemory) error {
	if len(results) == 0 {
		return nil
	}
	ids := make([]int64, len(results))
	for i, r := range results {
		ids[i] = r.ID
	}
	if err := s.store.RecordAccess(ctx, ids, s.clock()); err != nil {
		return fmt.Errorf("record access: %w", err)
	}
	return nil
}
