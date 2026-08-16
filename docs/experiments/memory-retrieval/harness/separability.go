package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
)

// Pair is one before-and-after statement from the memory-creation round-4
// pair set, which `harness/pairs_from_round4.py` writes out unchanged.
type Pair struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// PairSet is the committed pair set: four families, 250 pairs each, and
// which of them a supersession threshold is supposed to retire.
type PairSet struct {
	Seed          int               `json:"seed"`
	Source        string            `json:"source"`
	MustSupersede map[string]bool   `json:"must_supersede"`
	Families      map[string][]Pair `json:"families"`
}

// FamilyScores is one family's cosine distribution and what the shipped
// mechanism did with it.
type FamilyScores struct {
	Name          string  `json:"name"`
	MustSupersede bool    `json:"must_supersede"`
	Pairs         int     `json:"pairs"`
	Min           float64 `json:"min"`
	P25           float64 `json:"p25"`
	Median        float64 `json:"median"`
	P75           float64 `json:"p75"`
	Max           float64 `json:"max"`
	IQR           float64 `json:"iqr"`
	Mean          float64 `json:"mean"`
	// Superseded counts the pairs the shipped Store actually retired, by
	// storing the old statement and then the new one and reading
	// StoreResult.SupersededID. It is the mechanism's own answer, not a
	// prediction from the cosine column beside it.
	Superseded int `json:"superseded_by_shipped_store"`
}

// PairScore is one pair's outcome, kept so every figure below can be
// recomputed from the values that produced it. Index is the pair's
// position in its family in the committed pair set, which carries the
// statements themselves.
type PairScore struct {
	Family     string  `json:"family"`
	Index      int     `json:"index"`
	Cosine     float64 `json:"cosine"`
	Superseded bool    `json:"superseded_by_shipped_store"`
}

// Threshold is one row of the sweep over candidate thresholds.
type Threshold struct {
	Threshold          float64 `json:"threshold"`
	CorrectRetirements int     `json:"correct_retirements"`
	WrongRetirements   int     `json:"wrong_retirements"`
	Recall             float64 `json:"recall"`
	FalseSupersede     float64 `json:"false_supersede"`
	Accuracy           float64 `json:"accuracy"`
}

// Overlap describes where the family a threshold must retire sits on top
// of the family it must not.
type Overlap struct {
	SharedRange              []float64 `json:"shared_range"`
	CorrectionInSharedRange  float64   `json:"correction_in_shared_range"`
	CoexistInSharedRange     float64   `json:"coexist_in_shared_range"`
	CoexistAtOrAboveCorrect  float64   `json:"coexist_at_or_above_correction"`
	CoexistMinAboveShipped   bool      `json:"coexist_min_at_or_above_shipped_threshold"`
	CorrectionMedianBelowMin bool      `json:"correction_median_below_coexist_min"`
}

// Separability is the whole answer for one embedder.
type Separability struct {
	Embedder               string         `json:"embedder"`
	Dimensions             int            `json:"dimensions"`
	Seed                   int            `json:"seed"`
	PairsPerFamily         int            `json:"pairs_per_family"`
	SupersessionSimilarity float64        `json:"shipped_supersession_similarity"`
	Families               []FamilyScores `json:"families"`
	Overlap                Overlap        `json:"correction_coexist_overlap"`
	BestThreshold          Threshold      `json:"best_possible_threshold"`
	ShippedThreshold       Threshold      `json:"shipped_threshold"`
	ADRThreshold           Threshold      `json:"adr_0006_threshold"`
	// StoreAgreesWithCosine is the count of pairs where the shipped
	// Store's retire-or-not matches the harness cosine read against
	// SupersessionSimilarity. It validates the cosine column: the sweep
	// below is only meaningful if the number it sweeps is the number the
	// mechanism reads.
	StoreAgreesWithCosine int         `json:"store_agrees_with_cosine"`
	PairsMeasured         int         `json:"pairs_measured"`
	Sweep                 []Threshold `json:"sweep"`
	Scores                []PairScore `json:"scores"`
}

// familyOrder fixes the reporting order, matching the memory-creation
// record's table so the two can be read side by side.
var familyOrder = []string{"correction", "coexist", "cross-subject", "unrelated"}

// LoadPairSet reads the committed pair set.
func LoadPairSet(path string) (PairSet, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return PairSet{}, fmt.Errorf("read pair set: %w", err)
	}
	var set PairSet
	if err := json.Unmarshal(body, &set); err != nil {
		return PairSet{}, fmt.Errorf("decode pair set: %w", err)
	}
	if len(set.Families) == 0 {
		return PairSet{}, fmt.Errorf("pair set %s holds no families", path)
	}
	return set, nil
}

// MeasureSeparability asks the memory-creation question of one embedder:
// do the pairs that must both survive score at or above the corrections
// that must not? It reports the cosine distribution per family, the
// threshold sweep over it, and — for an embedder the store can run — what
// the shipped supersession mechanism actually did with each pair.
func MeasureSeparability(ctx context.Context, set PairSet, embedder Embedder, now time.Time) (Separability, error) {
	dims, err := embedder.dimensions(ctx)
	if err != nil {
		return Separability{}, err
	}
	out := Separability{
		Embedder:               embedder.Name,
		Dimensions:             dims,
		Seed:                   set.Seed,
		SupersessionSimilarity: memory.SupersessionSimilarity,
	}

	scores := map[string][]float64{}
	for _, name := range familyOrder {
		pairs, ok := set.Families[name]
		if !ok {
			return Separability{}, fmt.Errorf("pair set has no %q family", name)
		}
		family := FamilyScores{Name: name, MustSupersede: set.MustSupersede[name], Pairs: len(pairs)}
		for i, pair := range pairs {
			cos, superseded, err := pairOutcome(ctx, embedder, pair, now)
			if err != nil {
				return Separability{}, fmt.Errorf("%s pair: %w", name, err)
			}
			scores[name] = append(scores[name], cos)
			out.Scores = append(out.Scores, PairScore{Family: name, Index: i,
				Cosine: cos, Superseded: superseded})
			if superseded {
				family.Superseded++
			}
			if superseded == (cos >= memory.SupersessionSimilarity) {
				out.StoreAgreesWithCosine++
			}
			out.PairsMeasured++
		}
		quantiles(&family, scores[name])
		out.Families = append(out.Families, family)
		out.PairsPerFamily = len(pairs)
	}

	var positives, negatives []float64
	for _, name := range familyOrder {
		if set.MustSupersede[name] {
			positives = append(positives, scores[name]...)
			continue
		}
		negatives = append(negatives, scores[name]...)
	}
	for i := range 101 {
		row := thresholdRow(float64(i)/100, positives, negatives)
		out.Sweep = append(out.Sweep, row)
		if row.Accuracy > out.BestThreshold.Accuracy {
			out.BestThreshold = row
		}
	}
	out.ShippedThreshold = thresholdRow(memory.SupersessionSimilarity, positives, negatives)
	out.ADRThreshold = thresholdRow(0.70, positives, negatives)
	out.Overlap = overlapOf(scores["correction"], scores["coexist"])
	return out, nil
}

// pairOutcome returns the pair's embedding cosine and whether the shipped
// store superseded the old statement when the new one was written. The
// store holds exactly one memory when the second write lands, so its
// supersession lookup can only compare the pair with itself.
func pairOutcome(ctx context.Context, embedder Embedder, pair Pair, now time.Time) (float64, bool, error) {
	oldVec, err := embedder.Embedder.Embed(ctx, pair.Old)
	if err != nil {
		return 0, false, fmt.Errorf("embed %q: %w", pair.Old, err)
	}
	newVec, err := embedder.Embedder.Embed(ctx, pair.New)
	if err != nil {
		return 0, false, fmt.Errorf("embed %q: %w", pair.New, err)
	}

	dir, err := os.MkdirTemp("", "separability")
	if err != nil {
		return 0, false, fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(dir)
	store, err := sqlite.Open(filepath.Join(dir, "memory.db"))
	if err != nil {
		return 0, false, fmt.Errorf("open store: %w", err)
	}
	defer store.Close()

	svc := memory.NewService(store, embedder.Embedder, memory.NewConcatSummarizer(),
		memory.WithClock(func() time.Time { return now }))
	for _, content := range []string{pair.Old, pair.New} {
		res, err := svc.Store(ctx, memory.StoreInput{
			Content:         content,
			Type:            memory.TypeSemantic,
			ImportanceScore: salienceAddSemantic,
			Source:          "harness",
		})
		if err != nil {
			return 0, false, fmt.Errorf("store %q: %w", content, err)
		}
		if content == pair.New {
			return pairCosine(oldVec, newVec), res.SupersededID != 0, nil
		}
	}
	return 0, false, fmt.Errorf("pair produced no second write")
}

// pairCosine is the cosine similarity the supersession rule reads.
// internal/memory computes it with an unexported function, so the harness
// carries its own; Separability.StoreAgreesWithCosine reports whether the
// two agree on every pair measured.
func pairCosine(a, b []float32) float64 {
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

// quantiles fills the distribution fields, matching what
// memory-creation/harness/round4/separability.py reports so the two
// embedders' tables line up row for row.
func quantiles(family *FamilyScores, values []float64) {
	if len(values) == 0 {
		return
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	at := func(f float64) float64 {
		return sorted[min(len(sorted)-1, int(f*float64(len(sorted))))]
	}
	var sum float64
	for _, v := range sorted {
		sum += v
	}
	family.Min, family.Max = sorted[0], sorted[len(sorted)-1]
	family.P25, family.Median, family.P75 = at(0.25), at(0.5), at(0.75)
	family.IQR = family.P75 - family.P25
	family.Mean = sum / float64(len(sorted))
}

// thresholdRow scores one candidate threshold: what it retires correctly,
// and what it retires that must have survived.
func thresholdRow(t float64, positives, negatives []float64) Threshold {
	var tp, tn int
	for _, s := range positives {
		if s >= t {
			tp++
		}
	}
	for _, s := range negatives {
		if s < t {
			tn++
		}
	}
	total := len(positives) + len(negatives)
	return Threshold{
		Threshold:          t,
		CorrectRetirements: tp,
		WrongRetirements:   len(negatives) - tn,
		Recall:             float64(tp) / float64(len(positives)),
		FalseSupersede:     1 - float64(tn)/float64(len(negatives)),
		Accuracy:           float64(tp+tn) / float64(total),
	}
}

// overlapOf reports how far the coexist family sits above the corrections
// a threshold is meant to separate it from.
func overlapOf(correction, coexist []float64) Overlap {
	if len(correction) == 0 || len(coexist) == 0 {
		return Overlap{}
	}
	lo := math.Max(slicesMin(correction), slicesMin(coexist))
	hi := math.Min(slicesMax(correction), slicesMax(coexist))
	var inverted, inCorrection, inCoexist int
	for _, c := range correction {
		if lo <= c && c <= hi {
			inCorrection++
		}
		for _, x := range coexist {
			if x >= c {
				inverted++
			}
		}
	}
	for _, x := range coexist {
		if lo <= x && x <= hi {
			inCoexist++
		}
	}
	out := Overlap{
		CorrectionInSharedRange: float64(inCorrection) / float64(len(correction)),
		CoexistInSharedRange:    float64(inCoexist) / float64(len(coexist)),
		CoexistAtOrAboveCorrect: float64(inverted) / float64(len(correction)*len(coexist)),
		CoexistMinAboveShipped:  slicesMin(coexist) >= memory.SupersessionSimilarity,
	}
	if lo <= hi {
		out.SharedRange = []float64{lo, hi}
	}
	sorted := append([]float64(nil), correction...)
	sort.Float64s(sorted)
	out.CorrectionMedianBelowMin = sorted[len(sorted)/2] < slicesMin(coexist)
	return out
}

func slicesMin(values []float64) float64 {
	out := values[0]
	for _, v := range values {
		out = math.Min(out, v)
	}
	return out
}

func slicesMax(values []float64) float64 {
	out := values[0]
	for _, v := range values {
		out = math.Max(out, v)
	}
	return out
}
