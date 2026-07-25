package memory

import (
	"math"
	"time"
)

// All ranking weights, budgets, and thresholds live here — the one place
// for tunables per docs/architecture/memory.md.
const (
	// WeightVector and WeightFTS blend embedding cosine with keyword
	// match into one relevance score.
	WeightVector = 0.7
	WeightFTS    = 0.3

	// Ranking weights for relevance, recency, importance, and access.
	WeightRelevance  = 1.0
	WeightRecency    = 0.5
	WeightImportance = 1.0
	WeightAccess     = 0.25

	// RecencyHalfLife is the age at which the recency term halves.
	RecencyHalfLife = 24 * time.Hour
	// AccessScale is the access_count at which the access term saturates.
	AccessScale = 10.0

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

// score is the single ranking function: relevance, recency, importance,
// and access, weighted. Lifecycle decisions (eviction, forget) call it
// with rel 0 — the same function, without a query.
func score(m Memory, rel float64, now time.Time) float64 {
	age := max(now.Sub(m.Timestamp), 0)
	recency := math.Exp(-math.Ln2 * age.Hours() / RecencyHalfLife.Hours())
	access := min(math.Log1p(float64(m.AccessCount))/math.Log1p(AccessScale), 1)
	return WeightRelevance*rel +
		WeightRecency*recency +
		WeightImportance*m.ImportanceScore +
		WeightAccess*access
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
