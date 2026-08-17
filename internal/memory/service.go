package memory

import (
	"context"
	"fmt"
	"time"
)

const (
	// DefaultCapacity caps the active (searchable) index.
	DefaultCapacity = 10_000
	// DefaultSearchLimit bounds what one Search or Similar call returns
	// when the caller passes no positive limit. Recall is bounded by
	// DefaultRecallTokenBudget instead, and by nothing else.
	DefaultSearchLimit = 10
)

// Store hides the persistence engine (ADR 0002 escape hatch). It owns
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

// Embedder converts text to an embedding vector (ADR 0004).
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// Summary is what a Summarizer produced: the text, and whether a model
// generated it. The deterministic stand-in ADR 0004 licenses so the
// pipeline runs offline reports Generated false, so a caller that may
// only keep model-generated content can tell without inspecting the
// text — and cannot hold a summary without having been told.
type Summary struct {
	Text      string
	Generated bool
}

// Summarizer compresses a set of memory contents into one parent text.
type Summarizer interface {
	Summarize(ctx context.Context, contents []string) (Summary, error)
}

// Service is the memory domain's behavior contract: the write operations
// behind the writeback block (Store, Update, Delete), platform maintenance
// (Compress, Forget, Consolidate, Retain), and the runtime queries (Search,
// Recall, Similar) the agent loop needs (ADR 0011).
type Service interface {
	// Store embeds and inserts a new memory. An insert retires nothing:
	// identity is not inferred from similarity (ADR 0018), so a
	// correction coexists with what it corrects. It evicts
	// lowest-score-first when the active index exceeds capacity.
	Store(ctx context.Context, in StoreInput) (StoreResult, error)
	// Search runs hybrid search and records access on the returned
	// memories. A non-positive limit falls back to DefaultSearchLimit.
	Search(ctx context.Context, query string, limit int) ([]ScoredMemory, error)
	// Update supersedes a memory: a new row is inserted and the old row
	// is archived with superseded_by/valid_to set. Never overwrites.
	Update(ctx context.Context, in UpdateInput) (UpdateResult, error)
	// Delete archives one active memory by ID, recording valid_to while
	// superseded_by stays 0 (retirement, not supersession). A missing or
	// already-archived ID returns ErrNotFound.
	Delete(ctx context.Context, id int64) error
	// Compress summarizes a set of episodic memories into one or more
	// semantic parents — the summary is chunked so each parent stays
	// within SummaryMaxLen — and archives the originals.
	Compress(ctx context.Context, ids []int64) (CompressResult, error)
	// Forget archives active memories whose lifecycle score falls below
	// threshold. A non-positive threshold uses DefaultForgetThreshold.
	Forget(ctx context.Context, threshold float64) ([]int64, error)
	// Consolidate compresses the active episodic cluster into one or more
	// semantic parents once it reaches ConsolidationMinCluster; the
	// platform calls it on shutdown (ADR 0011).
	Consolidate(ctx context.Context) (CompressResult, error)
	// ConsolidateIfDue is the threshold trigger: it consolidates when the
	// active episodic count has crossed ConsolidationThreshold, and is a
	// no-op below it. The gateway evaluates it after a completed turn.
	ConsolidateIfDue(ctx context.Context) (CompressResult, error)
	// Retain is the tiered retention pass (ADR 0006): it archives active
	// memories whose salience tier has expired and keeps the rest. The
	// gateway evaluates it after a completed turn, alongside
	// ConsolidateIfDue.
	Retain(ctx context.Context) ([]int64, error)
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

// WithCapacity overrides DefaultCapacity (tests; production keeps the
// default).
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

// recordAccess marks returned memories as accessed (ADR 0006, Act step).
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
