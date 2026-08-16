// Command harness measures retrieval quality against the memory
// implementation this project ships. It plants a corpus with known
// ground truth into internal/memory's own store and queries it through
// internal/memory's own hybrid search, so the numbers in ../README.md
// describe the code that runs in production rather than a model of it.
//
// It measures two things, under whichever embedder is named: retrieval
// over the planted corpus, and whether any supersession threshold
// separates corrections from facts that must both survive.
//
// The default run is offline on the shipped hashing embedder and needs no
// API key:
//
//	go run ./docs/experiments/memory-retrieval/harness -seed 7
//
// The openai embedder reads its key from OPENAI_API_KEY in the
// environment and caches vectors by text at -cache, which must be a path
// outside the repository.
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
const probeCount = 120

// The measurements the command can run. Both is the default; the
// cache-replay embedder can only answer separability, because the
// corpus is not in the round-4 cache.
const (
	measureRetrieval    = "retrieval"
	measureSeparability = "separability"
	measureBoth         = "both"
)

// The three plantings measured. mixed-30d is the corpus as generated:
// the salience mix internal/agent/memory/writeback.go derives, over a 30-day
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

// Run is one seed's whole measurement, written to results/. Embedder and
// Dimensions head every run because a retrieval figure means nothing
// without them.
type Run struct {
	Seed         int64        `json:"seed"`
	Now          time.Time    `json:"now"`
	Embedder     string       `json:"embedder"`
	Dimensions   int          `json:"dimensions"`
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
	name := flag.String("embedder", embedderHashing,
		"embedder to measure: hashing, openai, or openai512-cache")
	cachePath := flag.String("cache", "",
		"vector cache file, outside the repository; required by openai and openai512-cache")
	measure := flag.String("measure", measureBoth, "what to measure: retrieval, separability, or both")
	record := flag.String("record", defaultRecord(), "the record whose tables -render rewrites")
	render := flag.Bool("render", false,
		"rewrite the record's tables from the committed results and measure nothing")
	flag.Parse()

	if *render {
		return renderRecord(*out, *record)
	}
	if *measure != measureRetrieval && *measure != measureSeparability && *measure != measureBoth {
		return fmt.Errorf("unknown measurement %q", *measure)
	}
	embedder, err := NewEmbedder(*name, *cachePath)
	if err != nil {
		return err
	}
	ctx := context.Background()

	corpus, err := GenerateCorpus(*seed, probeCount, storeSizes[len(storeSizes)-1], storeSizes[0])
	if err != nil {
		return fmt.Errorf("generate corpus: %w", err)
	}
	if err := writeJSON(filepath.Join(*out, fmt.Sprintf("corpus_seed%d.json", *seed)), corpus); err != nil {
		return err
	}

	if *measure != measureRetrieval {
		set, err := LoadPairSet(filepath.Join(*out, fmt.Sprintf("separability_pairs_seed%d.json", *seed)))
		if err != nil {
			return err
		}
		if err := embedder.prefetchAll(ctx, pairTexts(set)); err != nil {
			return err
		}
		started := time.Now()
		result, err := MeasureSeparability(ctx, set, embedder, now)
		if err != nil {
			return fmt.Errorf("separability: %w", err)
		}
		fmt.Printf("separability on %s: %d pairs in %s\n", embedder.Name, result.PairsMeasured,
			time.Since(started).Round(time.Millisecond))
		if err := writeJSON(filepath.Join(*out,
			fmt.Sprintf("separability_seed%d_%s.json", *seed, embedder.Name)), result); err != nil {
			return err
		}
		reportSeparability(result)
	}

	if *measure == measureSeparability {
		return renderIfRecordSeed(*seed, *out, *record)
	}
	if err := embedder.prefetchAll(ctx, corpusTexts(corpus)); err != nil {
		return err
	}
	dims, err := embedder.dimensions(ctx)
	if err != nil {
		return err
	}
	result := Run{
		Seed:       *seed,
		Now:        now,
		Embedder:   embedder.Name,
		Dimensions: dims,
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
			sized, err := Measure(ctx, planted, size, now, embedder)
			if err != nil {
				return fmt.Errorf("measure %s at %d: %w", arm.Name, size, err)
			}
			arm.Sizes = append(arm.Sizes, sized)
			fmt.Printf("%s %s size %d: %d probes in %s\n", embedder.Name, arm.Name, size,
				len(sized.Probes), time.Since(started).Round(time.Millisecond))
		}
		result.Arms = append(result.Arms, arm)
	}

	if err := writeJSON(filepath.Join(*out,
		fmt.Sprintf("retrieval_seed%d_%s.json", *seed, embedder.Name)), result); err != nil {
		return err
	}
	report(result)
	return renderIfRecordSeed(*seed, *out, *record)
}

// renderIfRecordSeed rewrites the record's tables after a run at the seed the
// record reports, so a measurement cannot leave the document behind it. A run
// at another seed writes its own result files and touches no record.
func renderIfRecordSeed(seed int64, out, record string) error {
	if seed != recordSeed {
		return nil
	}
	return renderRecord(out, record)
}

// corpusTexts is every text the retrieval measurement embeds: the planted
// facts and the task statements the probes query with.
func corpusTexts(corpus Corpus) []string {
	texts := make([]string, 0, len(corpus.Items)+len(corpus.Probes))
	for _, it := range corpus.Items {
		texts = append(texts, it.Content)
	}
	for _, probe := range corpus.Probes {
		texts = append(texts, probe.Task)
	}
	return texts
}

// pairTexts is every text the separability measurement embeds.
func pairTexts(set PairSet) []string {
	var texts []string
	for _, pairs := range set.Families {
		for _, pair := range pairs {
			texts = append(texts, pair.Old, pair.New)
		}
	}
	return texts
}

// reportSeparability prints the family table and what the thresholds do
// with it, naming the embedder on every line because the answer is only
// meaningful with it.
func reportSeparability(s Separability) {
	fmt.Printf("\n%s, %d dimensions, %d pairs per family\n", s.Embedder, s.Dimensions, s.PairsPerFamily)
	fmt.Println("family         supersede?  min    p25    median p75    max    iqr    store retired")
	for _, f := range s.Families {
		fmt.Printf("%-14s %-11t %.3f  %.3f  %.3f  %.3f  %.3f  %.3f  %d of %d\n",
			f.Name, f.MustSupersede, f.Min, f.P25, f.Median, f.P75, f.Max, f.IQR, f.Superseded, f.Pairs)
	}
	fmt.Printf("\ncoexist at or above correction: %.3f of cross pairs\n", s.Overlap.CoexistAtOrAboveCorrect)
	fmt.Printf("shipped threshold %.2f: %d correct, %d wrong retirements, accuracy %.3f\n",
		s.ShippedThreshold.Threshold, s.ShippedThreshold.CorrectRetirements,
		s.ShippedThreshold.WrongRetirements, s.ShippedThreshold.Accuracy)
	fmt.Printf("ADR 0006 threshold %.2f: %d correct, %d wrong retirements, accuracy %.3f\n",
		s.ADRThreshold.Threshold, s.ADRThreshold.CorrectRetirements,
		s.ADRThreshold.WrongRetirements, s.ADRThreshold.Accuracy)
	fmt.Printf("best possible threshold %.2f: %d correct, %d wrong retirements, accuracy %.3f\n",
		s.BestThreshold.Threshold, s.BestThreshold.CorrectRetirements,
		s.BestThreshold.WrongRetirements, s.BestThreshold.Accuracy)
	fmt.Printf("shipped Store agrees with the harness cosine on %d of %d pairs\n",
		s.StoreAgreesWithCosine, s.PairsMeasured)
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

// defaultRecord is the record whose tables this harness writes.
func defaultRecord() string {
	return filepath.Join("docs", "experiments", "memory-retrieval", "README.md")
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
		r.Seed, r.Probes, r.Embedder, r.Dimensions)
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
