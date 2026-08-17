package memory

import "time"

// The salience tiers and the retention bounds that read them live here —
// the one place for tunables per docs/architecture/memory.md and
// ADR 0006.
//
// What used to sit beside them went with the mechanism it belonged to.
// The vector and keyword weights, the cosine and the supersession
// threshold were the read path's, and an addressed store reads an
// address instead of scoring contents (#582). The eviction score and its
// half-life were an age-ordered rule under another name, and eviction is
// decided by last read now (#411, ADR 0018).
const (
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
	// what these set, and nothing else does.
	//
	// SalienceReplace is the salience of a correction — a write that
	// replaces the values already at its address.
	SalienceReplace = 0.8
	// SalienceJoin is the salience of a value added beside what is held.
	SalienceJoin = 0.7
)
