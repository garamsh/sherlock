package main

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/research/chained-recall/study"
)

// The retrieval at write (preregistration.md §Retrieval at write).
const (
	// kMessages is how many earlier source messages a batch's synthesis
	// is given: as many as C's window, from anywhere before the batch.
	kMessages = 8
	// kMids is how many earlier mids it is given.
	kMids = 4
)

// vec is one embedded unit the retrieval ranks: a source message, whose
// First and Last are its ID, or a mid over its source interval.
type vec struct {
	ID     int64
	First  int64
	Last   int64
	Text   string
	Vector []float32
}

// retrieved is what the search at write found for one batch, in source
// order.
type retrieved struct {
	Messages []vec
	Mids     []vec
}

// query is the text a batch searches with: the batch as the production
// store sends it to the summarizer, one message per line.
func query(batch []study.Message) string {
	return strings.Join(asSent(batch), "\n")
}

// asSent is how the production store sends a message to the summarizer:
// "role: content" after redaction.
func asSent(messages []study.Message) []string {
	out := make([]string, len(messages))
	for i, m := range messages {
		out[i] = m.Role + ": " + study.Redact(m.Content)
	}
	return out
}

// top ranks the units that end before the batch's first source by cosine
// to the query and keeps the k best, newest first on a tie as the
// production store's search orders them, and returns them in source order.
// A unit at or after the batch is never a candidate.
func top(target []float32, units []vec, first int64, k int) []vec {
	var candidates []vec
	for _, u := range units {
		if u.Last < first {
			candidates = append(candidates, u)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].First > candidates[j].First })
	scores := make([]float64, len(candidates))
	for i, u := range candidates {
		scores[i] = cosine(target, u.Vector)
	}
	index := make([]int, len(candidates))
	for i := range index {
		index[i] = i
	}
	sort.SliceStable(index, func(a, b int) bool { return scores[index[a]] > scores[index[b]] })
	out := make([]vec, 0, k)
	for _, i := range index[:min(k, len(index))] {
		out = append(out, candidates[i])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].First < out[j].First })
	return out
}

// retrieve is the search at write for one batch: the kMessages earlier
// source messages and the kMids earlier mids most similar to the batch.
func retrieve(ctx context.Context, embedder agent.Embedder, batch []study.Message, messages, mids []vec) (retrieved, error) {
	target, err := embedder.Embed(ctx, query(batch))
	if err != nil {
		return retrieved{}, fmt.Errorf("embed batch at %d: %w", batch[0].ID, err)
	}
	first := batch[0].ID
	return retrieved{Messages: top(target, messages, first, kMessages), Mids: top(target, mids, first, kMids)}, nil
}

// cosine is the production store's similarity (internal/memory/sqlite
// cosine, unexported); a zero vector is similar to nothing.
func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i, x := range a {
		y := float64(b[i])
		dot, na, nb = dot+float64(x)*y, na+float64(x)*float64(x), nb+y*y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}

// embedMessages embeds every source message as the summarizer is sent it.
func embedMessages(ctx context.Context, embedder agent.Embedder, messages []study.Message) ([]vec, error) {
	out := make([]vec, len(messages))
	for i, m := range messages {
		text := asSent([]study.Message{m})[0]
		v, err := embedder.Embed(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("embed source %d: %w", m.ID, err)
		}
		out[i] = vec{ID: m.ID, First: m.ID, Last: m.ID, Text: text, Vector: v}
	}
	return out, nil
}

// readMids reads every mid of a store with its summary and stored vector,
// in ID order.
func readMids(ctx context.Context, path string) ([]vec, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT id, source_first, source_last, summary, embedding FROM conversation_mid ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []vec
	for rows.Next() {
		var u vec
		var encoded []byte
		if err := rows.Scan(&u.ID, &u.First, &u.Last, &u.Text, &encoded); err != nil {
			return nil, err
		}
		if len(encoded) == 0 {
			return nil, fmt.Errorf("mid %d has no embedding", u.ID)
		}
		u.Vector = decodeVector(encoded)
		out = append(out, u)
	}
	return out, rows.Err()
}

// encodeVector is the production store's vector encoding
// (internal/memory/sqlite encodeVector, unexported): little-endian float32.
func encodeVector(vector []float32) []byte {
	out := make([]byte, 4*len(vector))
	for i, x := range vector {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(x))
	}
	return out
}

func decodeVector(encoded []byte) []float32 {
	out := make([]float32, len(encoded)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(encoded[4*i:]))
	}
	return out
}
