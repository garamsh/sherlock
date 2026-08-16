// Command harness measures retrieval quality against the memory
// implementation this project ships. It plants a corpus with known
// ground truth into internal/memory's own store and queries it through
// internal/memory's own hybrid search, so the numbers in ../README.md
// describe the code that runs in production rather than a model of it.
//
// It runs offline on the hashing embedder and needs no API key:
//
//	go run ./docs/experiments/memory-retrieval/harness -seed 7
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/garamsh/gagent/internal/memory"
)

// The run's shape. probeCount probes are drawn from the corpus; every
// store size holds all of them, so a size difference is a difference in
// distractor count and nothing else.
const (
	probeCount = 120
	embedder   = "hashing"
)

// The three plantings measured. mixed-30d is the corpus as generated:
// the salience mix internal/gateway/writeback.go derives, over a 30-day
// window. mixed-360d stretches that window twelvefold. flat holds
// salience and timestamp constant, which leaves the ranking function's
// relevance term alone to order the store.
const (
	armMixed30d  = "mixed-30d"
	armMixed360d = "mixed-360d"
	armFlat      = "flat"
	ageScale360d = 12
)

// storeSizes are the active-index sizes measured. The largest sits just
// under memory.DefaultCapacity, the ceiling the shipped configuration
// evicts at.
var storeSizes = []int{1_000, 4_000, 9_000}

// now is the fixed clock every run uses. The ranking function's recency
// term reads it, so it is pinned rather than taken from the wall clock.
var now = time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

// Config is the ranking configuration the run measured, read from the
// memory package so the record cannot state a value the code has moved.
type Config struct {
	WeightVector           float64 `json:"weight_vector"`
	WeightFTS              float64 `json:"weight_fts"`
	RecencyHalfLifeHours   float64 `json:"recency_half_life_hours"`
	SupersessionSimilarity float64 `json:"supersession_similarity"`
	EmbeddingDim           int     `json:"embedding_dim"`
	DefaultCapacity        int     `json:"default_capacity"`
	DefaultSearchLimit     int     `json:"default_search_limit"`
	RecallTokenBudget      int     `json:"default_recall_token_budget"`
	CharsPerToken          int     `json:"chars_per_token"`
}

// CorpusParams are the generator's inputs, so a reader can rebuild the
// corpus from the record alone.
type CorpusParams struct {
	Subjects       int `json:"subjects"`
	Topics         int `json:"topics"`
	MinFacts       int `json:"min_facts_per_pair"`
	MaxFacts       int `json:"max_facts_per_pair"`
	AgeWindowHours int `json:"age_window_hours"`
}

// Arm is one way of planting the same corpus. Every arm holds the same
// facts, the same probes and the same order; the arms differ only in the
// two things the ranking function reads besides relevance — the salience
// on each memory and how far its timestamp is spread.
type Arm struct {
	Name  string       `json:"name"`
	Sizes []SizeResult `json:"sizes"`
}

// Run is one seed's whole measurement, written to results/.
type Run struct {
	Seed         int64        `json:"seed"`
	Now          time.Time    `json:"now"`
	Embedder     string       `json:"embedder"`
	Probes       int          `json:"probes"`
	StoreSizes   []int        `json:"store_sizes"`
	CorpusParams CorpusParams `json:"corpus_params"`
	Config       Config       `json:"configuration"`
	Arms         []Arm        `json:"arms"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	seed := flag.Int64("seed", 7, "corpus seed")
	out := flag.String("out", defaultOut(), "directory the results are written to")
	flag.Parse()

	corpus, err := GenerateCorpus(*seed, probeCount, storeSizes[len(storeSizes)-1], storeSizes[0])
	if err != nil {
		return fmt.Errorf("generate corpus: %w", err)
	}
	result := Run{
		Seed:       *seed,
		Now:        now,
		Embedder:   embedder,
		Probes:     len(corpus.Probes),
		StoreSizes: storeSizes,
		CorpusParams: CorpusParams{
			Subjects:       len(subjects),
			Topics:         len(topics),
			MinFacts:       minFacts,
			MaxFacts:       maxFacts,
			AgeWindowHours: ageWindowHours,
		},
		Config: Config{
			WeightVector:           memory.WeightVector,
			WeightFTS:              memory.WeightFTS,
			RecencyHalfLifeHours:   memory.RecencyHalfLife.Hours(),
			SupersessionSimilarity: memory.SupersessionSimilarity,
			EmbeddingDim:           memory.EmbeddingDim,
			DefaultCapacity:        memory.DefaultCapacity,
			DefaultSearchLimit:     memory.DefaultSearchLimit,
			RecallTokenBudget:      memory.DefaultRecallTokenBudget,
			CharsPerToken:          memory.CharsPerToken,
		},
	}

	ctx := context.Background()
	for _, arm := range []Arm{
		{Name: armMixed30d},
		{Name: armMixed360d},
		{Name: armFlat},
	} {
		planted, err := plantingFor(arm.Name, corpus)
		if err != nil {
			return err
		}
		for _, size := range storeSizes {
			started := time.Now()
			sized, err := Measure(ctx, planted, size, now)
			if err != nil {
				return fmt.Errorf("measure %s at %d: %w", arm.Name, size, err)
			}
			arm.Sizes = append(arm.Sizes, sized)
			fmt.Printf("%s size %d: %d probes in %s\n", arm.Name, size, len(sized.Probes),
				time.Since(started).Round(time.Millisecond))
		}
		result.Arms = append(result.Arms, arm)
	}

	if err := writeJSON(filepath.Join(*out, fmt.Sprintf("corpus_seed%d.json", *seed)), corpus); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(*out, fmt.Sprintf("retrieval_seed%d.json", *seed)), result); err != nil {
		return err
	}
	report(result)
	return nil
}

// plantingFor returns the corpus as the named arm plants it.
func plantingFor(arm string, corpus Corpus) (Corpus, error) {
	switch arm {
	case armMixed30d:
		return corpus, nil
	case armMixed360d:
		return corpus.WithAgeScale(ageScale360d), nil
	case armFlat:
		return corpus.Flat(), nil
	default:
		return Corpus{}, fmt.Errorf("unknown arm %q", arm)
	}
}

// defaultOut is the results directory beside this harness.
func defaultOut() string {
	return filepath.Join("docs", "experiments", "memory-retrieval", "results")
}

func writeJSON(path string, value any) error {
	body, err := json.MarshalIndent(value, "", " ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// report prints every figure with its 95% interval, and what the interval
// would be at other probe counts.
func report(r Run) {
	fmt.Printf("\nseed %d, %d probes, %s embedder, %d-dimension vectors\n",
		r.Seed, r.Probes, r.Embedder, r.Config.EmbeddingDim)
	fmt.Println("\narm         store  metric               mean    95% interval")
	for _, arm := range r.Arms {
		for _, size := range arm.Sizes {
			f := size.Figures
			for _, row := range []struct {
				name string
				fig  Figure
			}{
				{"recall@1", f.RecallAt1}, {"precision@1", f.PrecisionAt1},
				{"recall@5", f.RecallAt5}, {"precision@5", f.PrecisionAt5},
				{"recall@10", f.RecallAtLimit}, {"precision@10", f.PrecisionAtLimit},
				{"recall@budget", f.RecallAtBudget}, {"returned@budget", f.ReturnedAtBudget},
				{"best rank", f.BestRank},
			} {
				fmt.Printf("%-11s %5d  %-17s %8.3f  [%.3f, %.3f]\n", arm.Name, size.Size,
					row.name, row.fig.Mean, row.fig.Mean-row.fig.Half, row.fig.Mean+row.fig.Half)
			}
		}
	}

	fmt.Println("\nwhere the width comes from, on recall@10 at the largest store of each arm")
	fmt.Println("arm         probes  95% half-width")
	for _, arm := range r.Arms {
		largest := arm.Sizes[len(arm.Sizes)-1].Figures.RecallAtLimit
		for _, probes := range []int{probeCount, 250, 500, 1000} {
			fmt.Printf("%-11s %6d  +/-%.3f\n", arm.Name, probes, HalfWidthAt(largest, probes))
		}
	}
}
