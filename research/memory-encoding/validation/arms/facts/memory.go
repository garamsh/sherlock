package main

import (
	"context"
	"math"
	"sort"

	"github.com/garamsh/sherlock/docs/research/chained-recall/study"
	"github.com/garamsh/sherlock/internal/agent"
)

// The survey's bounds on F's recall (../../../README.md §F).
const (
	// profileChars bounds the profile block: the newest user facts, whole,
	// until the next would pass it.
	profileChars = 1000
	// similarFacts is how many facts recall adds by cosine to the task.
	similarFacts = 3
)

type storedFact struct {
	ID          int64
	Kind        string
	Text        string
	Vector      []float32
	First, Last int64
}

// memory is the shipped recall with F's facts added after it.
type memory struct {
	study.ShippedMemory
	embedder agent.Embedder
	// facts are newest mid first, as readFacts orders them.
	facts []storedFact
	trace map[string]any
}

func item(f storedFact, kind string) study.Item {
	return study.Item{ID: f.ID, Level: "fact", Kind: kind, SourceFirst: f.First, SourceLast: f.Last, Text: f.Text}
}

// profile is the newest user facts whose texts fit in profileChars.
func (m *memory) profile() []study.Item {
	var out []study.Item
	used := 0
	for _, f := range m.facts {
		if f.Kind != "user" {
			continue
		}
		n := len([]rune(f.Text))
		if used+n > profileChars {
			break
		}
		used += n
		out = append(out, item(f, "profile"))
	}
	return out
}

// similar is the facts most similar to the task, those already delivered
// left out, at most similarFacts.
func (m *memory) similar(ctx context.Context, task string, delivered map[int64]bool) ([]study.Item, error) {
	if len(m.facts) == 0 {
		return nil, nil
	}
	q, err := m.embedder.Embed(ctx, task)
	if err != nil {
		return nil, err
	}
	type scored struct {
		f     storedFact
		score float64
	}
	var ranked []scored
	for _, f := range m.facts {
		if !delivered[f.ID] {
			ranked = append(ranked, scored{f, cosine(q, f.Vector)})
		}
	}
	// Newest first among equals, as the production search ranks mids.
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	var out []study.Item
	for _, s := range ranked {
		if len(out) == similarFacts {
			break
		}
		out = append(out, item(s.f, "fact"))
	}
	return out, nil
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		if i >= len(b) {
			break
		}
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}

// Recall is the shipped recall, then the profile block, then the similar
// facts.
func (m *memory) Recall(ctx context.Context, task string) ([]study.Item, error) {
	items, err := m.ShippedMemory.Recall(ctx, task)
	if err != nil {
		return nil, err
	}
	profile := m.profile()
	delivered := map[int64]bool{}
	for _, it := range profile {
		delivered[it.ID] = true
	}
	similar, err := m.similar(ctx, task, delivered)
	if err != nil {
		return nil, err
	}
	m.trace = map[string]any{"mids": len(items), "profile": len(profile), "similar_facts": len(similar)}
	return append(append(items, profile...), similar...), nil
}

// Search is the shipped memory_search.
func (m *memory) Search(ctx context.Context, query string, limit int) ([]study.Item, error) {
	m.trace = map[string]any{}
	return m.ShippedMemory.Search(ctx, query, limit)
}

func (m *memory) LastTrace() map[string]any { return m.trace }
