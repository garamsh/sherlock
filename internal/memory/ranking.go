package memory

import (
	"math"
	"time"
)

// All salience tiers, retention bounds and the one scoring function live
// here — the one place for tunables per docs/architecture/memory.md and
// ADR 0006.
//
// What used to sit beside them went with the store it belonged to
// (#582): the vector and keyword weights, the cosine, and the
// supersession threshold were the read path's, and an addressed store
// reads an address instead of scoring contents.
const (
	// RecencyHalfLife is the base half-life of the recency term; the
	// effective half-life scales with salience (ADR 0006).
	RecencyHalfLife = 24 * time.Hour

	// Retention tiers (ADR 0006): salience at or above
	// RetentionKeepSalience keeps a memory indefinitely; salience below
	// RetentionDropSalience drops the memory to the archive at the next
	// retention pass; between the two the memory lives for
	// RetentionWindow from its timestamp.
	RetentionKeepSalience = 0.6
	RetentionDropSalience = 0.3
	RetentionWindow       = 7 * 24 * time.Hour

	// Salience is platform-derived from the operation the write states,
	// never model-written (ADR 0006, ADR 0018). The tiers above read
	// what these set.
	//
	// SalienceReplace is the salience of a correction — a write that
	// replaces the values already at its address.
	SalienceReplace = 0.8
	// SalienceJoin is the salience of a value added beside what is held.
	SalienceJoin = 0.7
)

// score orders memories for eviction: salience plus an Ebbinghaus decay
// whose half-life scales with salience, so high-salience memories fade
// slowly and low-salience fast (ADR 0006).
//
// It takes no relevance term. An addressed read reaches its address
// without scoring anything, so the only caller left is the capacity
// eviction, which never had a query to score against.
func score(m Memory, now time.Time) float64 {
	age := max(now.Sub(m.Timestamp), 0)
	halfLife := RecencyHalfLife.Hours() * (1 + m.ImportanceScore)
	return m.ImportanceScore + math.Exp(-math.Ln2*age.Hours()/halfLife)
}
