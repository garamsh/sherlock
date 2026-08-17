package main

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"unicode"
)

// The similarity machinery the seam needs, and its only home.
//
// It was `internal/memory`'s until #582: the shipped store held an
// embedding per sentence and scored a read against it. The addressed
// store that replaced it computes where a fact is instead of scoring what
// it looks like, so it has no embedder, no cosine and no threshold — and
// ADR 0018 removes similarity as an identity test rather than weakening
// it, so nothing in `internal/` should grow one back.
//
// What still needs them is here. Three of the published designs this
// record ports model similarity as part of their own mechanism — `mem0`
// finds the memory a write applies to by it, and the two stream frames
// score a read by it — so a frame comparing them has to be able to
// compute one. That is a property of the designs being compared, not of
// the system being built.

// EmbeddingDim is the dimension of the vectors the hashing embedder
// produces.
const EmbeddingDim = 256

// mem0Gate is the cosine at or above which `mem0` treats a write as
// applying to an existing memory, and `identity` as a supersession.
//
// It is the value the shipped store used while it inferred identity from
// similarity. Nothing calibrates it — that is the finding
// `docs/experiments/memory-retrieval/` reached and ADR 0018 §Rejected
// alternatives records — and it is kept at the measured value so the
// ported designs are run at the number this project actually shipped
// rather than at one chosen to flatter or spoil them.
const mem0Gate = 0.6

// charsPerToken approximates tokens from content length, and
// recallTokenBudget is the token budget the shipped store's recall filled.
// Both describe the context a frame is allowed to inject, which is a
// property of the comparison rather than of any structure in it.
const (
	charsPerToken     = 4
	recallTokenBudget = 512
)

// Embedder converts text to an embedding vector. It is the seam
// `EmbedCounter` wraps to count what a frame spends on embedding.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// HashingEmbedder is the deterministic local embedder: each token is
// hashed into a signed slot of a fixed-size vector, which is then
// L2-normalized. It runs offline and needs no external API.
type HashingEmbedder struct{}

// NewHashingEmbedder returns the stateless hashing embedder.
func NewHashingEmbedder() HashingEmbedder { return HashingEmbedder{} }

// Embed implements Embedder.
func (HashingEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	vec := make([]float32, EmbeddingDim)
	for _, tok := range embedTokens(text) {
		h := fnv.New64a()
		_, _ = h.Write([]byte(tok))
		sum := h.Sum64()
		sign := float32(1)
		if sum&(1<<63) != 0 {
			sign = -1
		}
		vec[sum%EmbeddingDim] += sign
	}
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	if norm == 0 {
		return vec, nil
	}
	inv := float32(1 / math.Sqrt(norm))
	for i := range vec {
		vec[i] *= inv
	}
	return vec, nil
}

// embedTokens splits text into lowercase alphanumeric tokens.
func embedTokens(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// cosine is the similarity between two embeddings. The embedder above
// returns unit vectors, so the dot product is the cosine.
func cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var sum float32
	for i := range a {
		sum += a[i] * b[i]
	}
	return float64(sum)
}
