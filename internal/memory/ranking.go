package memory

import (
	"math"
	"time"
)

// All ranking weights, budgets, and thresholds live here — the one place
// for tunables per docs/architecture/memory.md and ADR 0006.
const (
	// WeightVector and WeightFTS blend embedding cosine with keyword
	// match into one relevance score.
	WeightVector = 0.7
	WeightFTS    = 0.3

	// RecencyHalfLife is the base half-life of the recency term; the
	// effective half-life scales with salience (ADR 0006).
	RecencyHalfLife = 24 * time.Hour

	// SupersessionSimilarity is the embedding cosine at or above which a
	// newly stored memory supersedes its most similar active predecessor
	// (ADR 0006).
	SupersessionSimilarity = 0.7

	// Retention tiers (ADR 0006): salience at or above
	// RetentionKeepSalience keeps a memory indefinitely; salience below
	// RetentionDropSalience drops the memory to the archive at the next
	// retention pass; between the two the memory lives for
	// RetentionWindow from its timestamp.
	RetentionKeepSalience = 0.6
	RetentionDropSalience = 0.3
	RetentionWindow       = 7 * 24 * time.Hour

	// DefaultForgetThreshold is the lifecycle score below which Forget
	// archives a memory when the caller passes no threshold.
	DefaultForgetThreshold = 0.15

	// DefaultRecallTokenBudget bounds the tokens Recall injects.
	DefaultRecallTokenBudget = 512
	// CharsPerToken approximates tokens from content length.
	CharsPerToken = 4
)

// relevance blends vector cosine with the normalized FTS score.
func relevance(cosine, ftsNorm float64) float64 {
	return WeightVector*cosine + WeightFTS*ftsNorm
}

// score is the single ranking function (ADR 0006): salience + relevance +
// recency, where recency is an Ebbinghaus decay whose half-life scales
// with salience — high-salience memories fade slowly, low-salience fast.
// Lifecycle decisions (eviction, forget) call it with rel 0 — the same
// function, without a query.
func score(m Memory, rel float64, now time.Time) float64 {
	age := max(now.Sub(m.Timestamp), 0)
	halfLife := RecencyHalfLife.Hours() * (1 + m.ImportanceScore)
	recency := math.Exp(-math.Ln2 * age.Hours() / halfLife)
	return m.ImportanceScore + rel + recency
}

// cosine returns the cosine similarity of two vectors, 0 when either has
// no magnitude or the dimensions differ.
func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// estimateTokens approximates the token cost of a memory's content.
func estimateTokens(content string) int {
	return max(len(content)/CharsPerToken, 1)
}
