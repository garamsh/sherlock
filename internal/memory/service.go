package memory

import (
	"context"
	"fmt"
	"time"
)

// DefaultCapacity caps the active (readable) index.
const DefaultCapacity = 10_000

// Store hides the persistence engine (ADR 0002 escape hatch). It owns
// rows and indexes only; what a read reaches is decided by the address it
// is given, so the store holds no scoring and no ordering over addresses.
//
// The three read methods are one hierarchy — an address, the question it
// asks, the subject it is about — and each is one query. Nothing here
// scans what the store holds to answer.
type Store interface {
	// Write stores m and archives supersede against it, in one
	// transaction, and returns m's ID and the IDs actually archived. A
	// value already active at m's address is not stored twice: its
	// timestamp moves to the new write and its existing ID comes back,
	// so restating a fact holds it once — and that row is never one of
	// the rows archived, whether or not the caller named it.
	//
	// It is one method rather than an insert and a loop because a write
	// that landed while its supersessions did not would leave a value
	// and its correction both active at one address.
	Write(ctx context.Context, m Memory, supersede []int64, at time.Time) (int64, []int64, error)
	// At returns the active memories held at exactly one address.
	At(ctx context.Context, a Address) ([]Memory, error)
	// UnderQuestion returns the active memories at every address with
	// this subject and question — the question asked and the
	// restrictions under it (ADR 0018).
	UnderQuestion(ctx context.Context, subject, question string) ([]Memory, error)
	// UnderSubject returns the active memories at every address under
	// one subject.
	UnderSubject(ctx context.Context, subject string) ([]Memory, error)
	// Active returns every active memory. Retention and capacity
	// eviction read it; no read of the store's contents answers a
	// question.
	Active(ctx context.Context) ([]Memory, error)
	// Archive moves an active memory to the archive table, recording
	// supersededBy (0 when not superseded) and validTo.
	Archive(ctx context.Context, id int64, supersededBy int64, validTo time.Time) error
	// RecordAccess increments access_count and sets last_accessed on the
	// given active memories.
	RecordAccess(ctx context.Context, ids []int64, at time.Time) error
	// Close releases the store.
	Close() error
}

// Service is the memory domain's behavior contract: the write the
// writeback block makes (Store), the two addressed reads, and the
// after-turn retention pass the gateway evaluates (ADR 0011).
//
// Store is the only write, because it is the only one the writeback can
// state. An addressed write carries an address, a value and an operation
// (ADR 0018), so it names no memory ID.
//
// Neither read takes a query in words. ADR 0018 §What ships is three
// parts: asked in words an addressed store answers nothing at all, and
// the step that reads a question into an address is #336 step 3, in front
// of this. Until it lands the agent loop calls neither of these — the
// callers are the tests and the measurement, which supply the address the
// way the record's figures were produced.
type Service interface {
	// Store writes one value at one address. OpReplace supersedes what
	// is held there; OpJoin leaves it and adds one. It evicts
	// lowest-score-first when the active index exceeds capacity.
	Store(ctx context.Context, in StoreInput) (StoreResult, error)
	// At answers one address: the values held there and, where the
	// address restricts nothing, the values at every address that
	// restricts the same question (ADR 0018). Access is recorded on
	// what comes back.
	At(ctx context.Context, a Address) ([]Memory, error)
	// UnderSubject answers a question that names a subject and no
	// question. Every address under the subject comes back, in no
	// order: an addressed store has no ranking over addresses and no
	// budget to stop at, so where that is more than fits it returns
	// more than fits (ADR 0018 §What is not measured).
	UnderSubject(ctx context.Context, subject string) ([]Memory, error)
	// Retain is the tiered retention pass (ADR 0006): it archives active
	// memories whose salience tier has expired and keeps the rest. The
	// gateway evaluates it after a completed turn.
	Retain(ctx context.Context) ([]int64, error)
	// Close releases the underlying store.
	Close() error
}

type service struct {
	store    Store
	clock    func() time.Time
	capacity int
}

// Option customizes a service; defaults come from the package constants.
type Option func(*service)

// WithClock substitutes the time source (tests).
func WithClock(clock func() time.Time) Option {
	return func(s *service) { s.clock = clock }
}

// WithCapacity overrides DefaultCapacity (tests; production keeps the
// default). A negative capacity is clamped to zero, which evicts
// everything, rather than being read as a cap the eviction can slice
// past.
func WithCapacity(capacity int) Option {
	return func(s *service) { s.capacity = max(capacity, 0) }
}

// NewService returns the memory service over the given store.
func NewService(store Store, opts ...Option) Service {
	s := &service{
		store:    store,
		clock:    time.Now,
		capacity: DefaultCapacity,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Close closes the underlying store.
func (s *service) Close() error { return s.store.Close() }

// validate rejects writes that would corrupt the store. Agent input is
// already schema-validated at the tool boundary; this guards the module
// boundary for direct runtime callers.
//
// A restriction is not required: a sentence that narrows nothing places
// none, and rejecting the empty one would reject most writes.
func validate(in StoreInput) error {
	if in.Address.Subject == "" {
		return fmt.Errorf("%w: empty subject", ErrInvalidInput)
	}
	if in.Address.Question == "" {
		return fmt.Errorf("%w: empty question", ErrInvalidInput)
	}
	if in.Value == "" {
		return fmt.Errorf("%w: empty value", ErrInvalidInput)
	}
	if in.Operation != OpReplace && in.Operation != OpJoin {
		return fmt.Errorf("%w: unknown operation %q", ErrInvalidInput, in.Operation)
	}
	return nil
}

// recordAccess marks returned memories as accessed (ADR 0006, Act step).
// ADR 0018 §Forgetting is decided by use rests on this being written on
// every read, not on the reads a caller finds interesting.
func (s *service) recordAccess(ctx context.Context, held []Memory) error {
	if len(held) == 0 {
		return nil
	}
	ids := make([]int64, len(held))
	for i, m := range held {
		ids[i] = m.ID
	}
	if err := s.store.RecordAccess(ctx, ids, s.clock()); err != nil {
		return fmt.Errorf("record access: %w", err)
	}
	return nil
}
