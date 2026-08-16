package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/garamsh/gagent/internal/memory"
)

// mem0Candidates is how many existing memories Mem0's update phase puts in
// front of the decision, stated in Chhikara et al. 2025 (arXiv 2504.19413,
// §Mem0 Architecture): the s most similar, with s = 10.
const mem0Candidates = 10

// mem0Frame is Mem0's two-phase memory: a flat set of extracted facts, and
// a write that retrieves the most similar existing memories and applies an
// operation to one of them — add, update, delete or no-op.
//
// It is here because it is the published design closest to what this record
// recommends and it differs from it in exactly one decision. Both take the
// write operation from a model reading the sentence. This one then has to
// find which memory the operation applies to, and it finds it by similarity;
// the addressed store computes the address and goes there. Holding the
// declaration constant across the two is what separates identity from arity,
// which no run in this record had separated before.
//
// What the seam withholds and this design needs is a model in the write
// path, twice over: once to extract the fact, which the oracle arm supplies
// and the text arm does not, and once to judge whether a candidate memory is
// the one the new fact updates. The second is substituted here by a
// similarity gate at the constant this project's own store supersedes at,
// which is in the run table. §What a published design needed reports what
// that substitution can and cannot decide.
type mem0Frame struct {
	embedder memory.Embedder
	items    []*mem0Item
	// gate is the similarity at or above which a candidate is taken to be
	// the memory the write updates.
	gate float64
}

// mem0Item is one stored memory: the fact as the frame would emit it, and
// the vector its retrieval finds it by.
type mem0Item struct {
	text   string
	vector []float32
}

// NewMem0Frame returns the two-phase frame with the judgement substituted by
// a gate at the shipped supersession similarity — the same judgement this
// project's own store makes, at the constant the run table reports.
func NewMem0Frame(counter *EmbedCounter) Frame {
	return &mem0Frame{embedder: counter.Wrap(memory.NewHashingEmbedder()),
		gate: memory.SupersessionSimilarity}
}

// NewMem0NearestFrame returns the same design with the gate removed: an
// update takes the nearest candidate whatever its similarity.
//
// The paper states no threshold, because the model it puts the candidates in
// front of decides. This arm and the gated one are therefore the two ends
// the design has without that model, the way attribute-single and
// attribute-multi are the two ends of the arity question, and the design
// sits between them at a point nothing here can locate.
func NewMem0NearestFrame(counter *EmbedCounter) Frame {
	return &mem0Frame{embedder: counter.Wrap(memory.NewHashingEmbedder())}
}

// Observe runs the update phase for each of the turn's assertions: retrieve
// the most similar memories, and apply the declared operation to the nearest
// of them that the gate accepts.
//
// A turn carrying no assertion writes nothing. Mem0's extraction phase is a
// model reading the message, so a frame handed no extraction has no
// candidate fact — which is why this design holds nothing in the text arm,
// as every other extraction-dependent structure here does.
func (f *mem0Frame) Observe(ctx context.Context, t Turn) error {
	for _, written := range t.Written {
		if err := f.update(ctx, written); err != nil {
			return fmt.Errorf("update at turn %d: %w", t.Index, err)
		}
	}
	return nil
}

// update applies one assertion. The declaration says what the write does and
// the retrieval says what it does it to.
func (f *mem0Frame) update(ctx context.Context, written Written) error {
	text := line(written.Address, written.Value)
	vector, err := f.embedder.Embed(ctx, text)
	if err != nil {
		return fmt.Errorf("embed: %w", err)
	}
	candidates := f.nearest(vector)
	for _, index := range candidates {
		if f.items[index].text == text {
			// The no-op: the memory is already among the candidates.
			return nil
		}
	}
	// An update ends the nearest candidate, and only when the gate accepts
	// it. Where it does not, there is nothing this write can be an update to
	// and the design adds.
	if written.Declared == OperationReplace && len(candidates) > 0 {
		nearest := candidates[0]
		if cosine(vector, f.items[nearest].vector) >= f.gate {
			f.items[nearest] = &mem0Item{text: text, vector: vector}
			return nil
		}
	}
	f.items = append(f.items, &mem0Item{text: text, vector: vector})
	return nil
}

// nearest is the indices of the candidates the update phase considers, most
// similar first.
func (f *mem0Frame) nearest(vector []float32) []int {
	order := make([]int, len(f.items))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return cosine(vector, f.items[order[a]].vector) > cosine(vector, f.items[order[b]].vector)
	})
	return order[:min(mem0Candidates, len(order))]
}

// Context returns the memories most similar to what is asked, up to the
// candidate count the design retrieves at.
//
// The query is the address where a form supplies one, so this frame is
// handed exactly what every addressed frame is handed and the comparison
// between them is about what a structure does with an address rather than
// about what it knows. Where none arrives it queries with the question's
// words, which is what its retrieval takes in production — this design is
// the one fact-holding structure here with a read path that a worded
// question reaches at all.
//
// A question naming a subject and no attribute is one more query string.
// Where an addressed store answers such a question by reading every address
// under the subject and returning all of them, this one ranks and stops at
// the count it retrieves at, whatever the question spanned.
func (f *mem0Frame) Context(ctx context.Context, t Turn) (Injected, error) {
	if len(f.items) == 0 {
		return Injected{}, nil
	}
	query := t.Text
	if t.Ask != nil {
		query = strings.TrimSpace(t.Ask.Subject + " " + t.Ask.Attribute)
	}
	vector, err := f.embedder.Embed(ctx, query)
	if err != nil {
		return Injected{}, fmt.Errorf("embed the query: %w", err)
	}
	var lines []string
	for _, index := range f.nearest(vector) {
		lines = append(lines, f.items[index].text)
	}
	return Injected{Text: strings.Join(lines, "\n"), Scanned: len(f.items)}, nil
}

// Size counts one unit per memory, each costing its text and the vector it
// is retrieved by.
func (f *mem0Frame) Size(context.Context) (Size, error) {
	out := Size{Units: len(f.items)}
	for _, item := range f.items {
		out.ContentBytes += int64(len(item.text))
	}
	out.StoredBytes = out.ContentBytes + int64(len(f.items))*vectorBytes
	return out, nil
}

// Held is every memory's text.
func (f *mem0Frame) Held(context.Context) ([]string, error) {
	out := make([]string, 0, len(f.items))
	for _, item := range f.items {
		out = append(out, item.text)
	}
	return out, nil
}

// Forget drops the memories whose text it is given.
func (f *mem0Frame) Forget(_ context.Context, held []string) error {
	victims := map[string]bool{}
	for _, text := range held {
		victims[text] = true
	}
	kept := f.items[:0]
	for _, item := range f.items {
		if !victims[item.text] {
			kept = append(kept, item)
		}
	}
	f.items = kept
	return nil
}

func (f *mem0Frame) Close() error { return nil }
