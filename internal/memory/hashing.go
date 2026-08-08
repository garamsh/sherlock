package memory

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"unicode"
)

// EmbeddingDim is the dimension of vectors the hashing embedder produces.
const EmbeddingDim = 256

// HashingEmbedder is the deterministic local embedder (ADR 0004): each
// token is hashed into a signed slot of a fixed-size vector, which is
// then L2-normalized. It runs offline and needs no external API; a real
// embedding model plugs into the same Embedder interface.
type HashingEmbedder struct{}

// NewHashingEmbedder returns the stateless hashing embedder.
func NewHashingEmbedder() HashingEmbedder { return HashingEmbedder{} }

// Embed implements Embedder.
func (HashingEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	vec := make([]float32, EmbeddingDim)
	for _, tok := range tokenize(text) {
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

// tokenize splits text into lowercase alphanumeric tokens. The FTS query
// builder uses the same tokenization so keyword and vector views of a
// text agree on its words.
func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}
