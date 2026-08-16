package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
)

// ProbeResult is one probe's outcome at one store size. Hits are counted
// by store ID: a returned memory is credited only when it is one of the
// rows the generator planted for this probe's subject-topic pair.
type ProbeResult struct {
	Task             string `json:"task"`
	Relevant         int    `json:"relevant"`
	HitsAt1          int    `json:"hits_at_1"`
	HitsAt5          int    `json:"hits_at_5"`
	HitsAtLimit      int    `json:"hits_at_limit"`
	ReturnedAtLimit  int    `json:"returned_at_limit"`
	HitsAtBudget     int    `json:"hits_at_budget"`
	ReturnedAtBudget int    `json:"returned_at_budget"`
	// BestRank is where the highest-ranked planted answer sits in the
	// whole ranked store, so a miss says whether the answer was outranked
	// narrowly or was nowhere near the top.
	BestRank int `json:"best_rank"`
	// ReturnedAgeHours and ReturnedSalience average the two fields the
	// ranking function reads besides relevance over what it returned.
	// They read against the store's own means in the enclosing size.
	ReturnedAgeHours float64 `json:"returned_age_hours"`
	ReturnedSalience float64 `json:"returned_salience"`
}

// SizeResult is every probe run against one store size, with the figures
// those probes produce.
type SizeResult struct {
	Size            int     `json:"size"`
	RelevantPlanted int     `json:"relevant_planted"`
	StoreAgeHours   float64 `json:"store_mean_age_hours"`
	StoreSalience   float64 `json:"store_mean_salience"`
	// StoreContentChars is the mean content length, which decides
	// whether the recall token budget can bind before the search limit.
	StoreContentChars float64       `json:"store_mean_content_chars"`
	Figures           Figures       `json:"figures"`
	Probes            []ProbeResult `json:"probes"`
}

// Measure plants the corpus prefix of the given size into a fresh store
// and runs every probe through the shipped read path. The store, the
// embedder, the hybrid search and the ranking function are the ones
// internal/memory ships; the harness supplies only the corpus and the
// clock.
func Measure(ctx context.Context, corpus Corpus, size int, now time.Time) (SizeResult, error) {
	if size > len(corpus.Items) {
		return SizeResult{}, fmt.Errorf("store size %d exceeds the %d-item corpus", size, len(corpus.Items))
	}
	dir, err := os.MkdirTemp("", "retrieval-harness")
	if err != nil {
		return SizeResult{}, fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	store, err := sqlite.Open(filepath.Join(dir, "memory.db"))
	if err != nil {
		return SizeResult{}, fmt.Errorf("open store: %w", err)
	}
	defer store.Close()

	embedder := memory.NewHashingEmbedder()
	items := corpus.Items[:size]
	ids, err := plant(ctx, store, embedder, items, now)
	if err != nil {
		return SizeResult{}, err
	}

	svc := memory.NewService(store, embedder, memory.NewConcatSummarizer(),
		memory.WithClock(func() time.Time { return now }))

	out := SizeResult{Size: size}
	for _, it := range items {
		out.StoreAgeHours += float64(it.AgeHours) / float64(len(items))
		out.StoreSalience += it.Salience / float64(len(items))
		out.StoreContentChars += float64(len(it.Content)) / float64(len(items))
	}
	for _, probe := range corpus.Probes {
		relevant := make(map[int64]bool)
		for i, it := range items {
			if probe.Answers(it) {
				relevant[ids[i]] = true
			}
		}
		if len(relevant) == 0 {
			return SizeResult{}, fmt.Errorf("probe %q has no planted answer in a store of %d", probe.Task, size)
		}
		out.RelevantPlanted += len(relevant)

		ranked, err := svc.Search(ctx, probe.Task, 0)
		if err != nil {
			return SizeResult{}, fmt.Errorf("search %q: %w", probe.Task, err)
		}
		budgeted, err := svc.Recall(ctx, probe.Task, 0)
		if err != nil {
			return SizeResult{}, fmt.Errorf("recall %q: %w", probe.Task, err)
		}
		// Similar runs the same ranking core as Search over the whole
		// store; unlike Search it records no access, so asking where the
		// answers rank does not change what the next probe sees.
		full, err := svc.Similar(ctx, probe.Task, size)
		if err != nil {
			return SizeResult{}, fmt.Errorf("rank %q: %w", probe.Task, err)
		}
		out.Probes = append(out.Probes, ProbeResult{
			Task:             probe.Task,
			Relevant:         len(relevant),
			HitsAt1:          hits(ranked, relevant, 1),
			HitsAt5:          hits(ranked, relevant, 5),
			HitsAtLimit:      hits(ranked, relevant, len(ranked)),
			ReturnedAtLimit:  len(ranked),
			HitsAtBudget:     hits(budgeted, relevant, len(budgeted)),
			ReturnedAtBudget: len(budgeted),
			BestRank:         bestRank(full, relevant),
			ReturnedAgeHours: meanOver(ranked, func(m memory.ScoredMemory) float64 {
				return now.Sub(m.Timestamp).Hours()
			}),
			ReturnedSalience: meanOver(ranked, func(m memory.ScoredMemory) float64 {
				return m.ImportanceScore
			}),
		})
	}
	out.Figures = Summarize(out)
	return out, nil
}

// plant writes the items straight to the store, returning one ID per item
// in the order given. It uses the store's insert rather than the memory
// service's Store so that supersession on insert cannot archive a planted
// row and leave the ground truth describing a store that no longer holds
// it; the write path is the memory-creation experiment's subject.
func plant(ctx context.Context, store *sqlite.Store, embedder memory.Embedder, items []Item, now time.Time) ([]int64, error) {
	ids := make([]int64, len(items))
	for i, it := range items {
		emb, err := embedder.Embed(ctx, it.Content)
		if err != nil {
			return nil, fmt.Errorf("embed item %d: %w", i, err)
		}
		id, err := store.Insert(ctx, memory.Memory{
			Content:         it.Content,
			Embedding:       emb,
			Type:            memory.Type(it.Type),
			ImportanceScore: it.Salience,
			Timestamp:       now.Add(-time.Duration(it.AgeHours) * time.Hour),
			Source:          "harness",
		})
		if err != nil {
			return nil, fmt.Errorf("insert item %d: %w", i, err)
		}
		ids[i] = id
	}
	return ids, nil
}

// hits counts how many of the first k returned memories are planted
// answers, by store ID.
func hits(results []memory.ScoredMemory, relevant map[int64]bool, k int) int {
	var n int
	for i, r := range results {
		if i >= k {
			break
		}
		if relevant[r.ID] {
			n++
		}
	}
	return n
}

// meanOver averages one field of the returned memories.
func meanOver(results []memory.ScoredMemory, field func(memory.ScoredMemory) float64) float64 {
	if len(results) == 0 {
		return 0
	}
	var total float64
	for _, r := range results {
		total += field(r)
	}
	return total / float64(len(results))
}

// bestRank returns the 1-based position of the first planted answer in a
// ranking, or 0 when the ranking holds none.
func bestRank(results []memory.ScoredMemory, relevant map[int64]bool) int {
	for i, r := range results {
		if relevant[r.ID] {
			return i + 1
		}
	}
	return 0
}
