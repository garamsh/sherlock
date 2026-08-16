package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The separability half of the claim register. It compares the record's
// two tables and their findings with this experiment's committed results,
// and — where the record claims to reproduce memory-creation's published
// figures — with that experiment's committed results as well. The
// reproduction claim is the reason the comparison with the shipped
// embedder means anything, so it is checked rather than asserted.
const round4Results = "../../memory-creation/results_round4"

// round4Separability is the subset of memory-creation's committed
// separability file this register reads.
type round4Separability struct {
	Dims     int `json:"dims"`
	Families map[string]struct {
		Median float64 `json:"median"`
		IQR    float64 `json:"iqr"`
	} `json:"families"`
	Overlap struct {
		CoexistAtOrAboveCorrection float64 `json:"coexist_at_or_above_correction"`
	} `json:"correction_coexist_overlap"`
	Sweep []struct {
		T                  float64 `json:"t"`
		CorrectRetirements int     `json:"correct_retirements"`
		WrongRetirements   int     `json:"wrong_retirements"`
	} `json:"sweep"`
}

func (r round4Separability) at(t *testing.T, threshold float64) (int, int) {
	t.Helper()
	for _, row := range r.Sweep {
		if math.Abs(row.T-threshold) < 1e-9 {
			return row.CorrectRetirements, row.WrongRetirements
		}
	}
	t.Fatalf("memory-creation's sweep has no row at %v", threshold)
	return 0, 0
}

func loadSeparability(t *testing.T, embedder string) Separability {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(resultsDir, "separability_seed7_"+embedder+".json"))
	require.NoError(t, err)
	var out Separability
	require.NoError(t, json.Unmarshal(body, &out))
	return out
}

func loadRound4(t *testing.T, name string) round4Separability {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(round4Results, name+".json"))
	require.NoError(t, err)
	var out round4Separability
	require.NoError(t, json.Unmarshal(body, &out))
	return out
}

func TestRecordSeparability_MatchesItsCommittedSource(t *testing.T) {
	r := newRegister(t)
	runs := map[string]Separability{}
	for _, embedder := range []string{embedderHashing, embedderOpenAI, embedderOpenAI512} {
		runs[embedder] = loadSeparability(t, embedder)
	}

	// The record's headline rests on these three holding for every embedder,
	// so they are checked as claims. The tables they summarise are generated
	// by record.go and are not read back here.
	for _, name := range separabilityEmbedders {
		result := runs[name]
		assert.True(t, result.Overlap.CorrectionMedianBelowMin,
			"%s: the correction median is no longer below the coexist minimum", name)
		assert.True(t, result.Overlap.CoexistMinAboveShipped,
			"%s: the coexist minimum is no longer at or above the shipped threshold", name)
		assert.Equal(t, result.PairsMeasured, result.StoreAgreesWithCosine,
			"%s: the shipped store and the harness cosine disagree on some pair", name)
	}

	a := r.anchor(`writes the ([\d,]+) pairs to ` + "`results/separability_pairs_seed7.json`" + `, (\d+) per family`)
	r.number("pairs measured", a[0], float64(runs[embedderHashing].PairsMeasured))
	r.number("pairs per family", a[1], float64(runs[embedderHashing].PairsPerFamily))

	a = r.anchor(`agree on ([\d,]+) of ([\d,]+) pairs under every embedder`)
	for name, result := range runs {
		r.number(name+" store agrees with cosine", a[0], float64(result.StoreAgreesWithCosine))
		r.number(name+" pairs measured", a[1], float64(result.PairsMeasured))
	}

	a = r.anchor(`in (\d+\.\d+) of the ([\d,]+) cross pairs, against (\d+\.\d+) at 512 and (\d+\.\d+) at 1536`)
	r.number("hashing coexist at or above correction", a[0],
		runs[embedderHashing].Overlap.CoexistAtOrAboveCorrect)
	r.number("cross pairs", a[1], float64(runs[embedderHashing].PairsPerFamily*runs[embedderHashing].PairsPerFamily))
	r.number("openai512 coexist at or above correction", a[2],
		runs[embedderOpenAI512].Overlap.CoexistAtOrAboveCorrect)
	r.number("openai coexist at or above correction", a[3],
		runs[embedderOpenAI].Overlap.CoexistAtOrAboveCorrect)

	a = r.anchor("`hashing`" + ` retires (\d+) of (\d+) corrections where ` + "`openai512-cache`" + ` retires (\d+)`)
	r.number("hashing corrections retired", a[0],
		float64(familyNamed(t, runs[embedderHashing], "correction").Superseded))
	r.number("corrections in the family", a[1],
		float64(familyNamed(t, runs[embedderHashing], "correction").Pairs))
	r.number("openai512 corrections retired", a[2],
		float64(familyNamed(t, runs[embedderOpenAI512], "correction").Superseded))

	hashingCorrection := familyNamed(t, runs[embedderHashing], "correction")
	a = r.anchor(`catches (\d+\.\d+)% of the corrections it exists for while destroying every coexisting fact and (\d+) of (\d+) statements about a different person`)
	r.number("share of corrections caught", a[0], 100*float64(hashingCorrection.Superseded)/float64(hashingCorrection.Pairs))
	r.number("cross-subject statements retired", a[1],
		float64(familyNamed(t, runs[embedderHashing], "cross-subject").Superseded))
	r.number("cross-subject pairs", a[2], float64(familyNamed(t, runs[embedderHashing], "cross-subject").Pairs))

	a = r.anchor(`the correction family's median is (\d+\.\d+) and its 75th percentile (\d+\.\d+) — (\d+\.\d+) is not where corrections land`)
	r.number("hashing correction median", a[0], hashingCorrection.Median)
	r.number("hashing correction p75", a[1], hashingCorrection.P75)
	r.number("the value ranking.go cites", a[2], 0.66)

	a = r.anchor(`its maximum being (\d+\.\d+)\. The coexist family's minimum on the same embedder is (\d+\.\d+)`)
	r.number("hashing correction maximum", a[0], hashingCorrection.Max)
	r.number("hashing coexist minimum", a[1], familyNamed(t, runs[embedderHashing], "coexist").Min)

	r.report()
	checkReproduction(t, r, runs)
}

// checkReproduction verifies the record's claim that running the committed
// round-4 vectors through this harness reproduces memory-creation's
// published figures, and the one place it does not.
func checkReproduction(t *testing.T, r *register, runs map[string]Separability) {
	t.Helper()
	replay, committed := runs[embedderOpenAI512], loadRound4(t, "separability")
	require.Equal(t, committed.Dims, replay.Dimensions)

	a := r.anchor(`correction (\d+\.\d+) median and (\d+\.\d+) IQR, coexist (\d+\.\d+) and (\d+\.\d+), ` +
		`cross-subject (\d+\.\d+) and (\d+\.\d+), unrelated (\d+\.\d+) and (\d+\.\d+), and (\d+) correct against (\d+) wrong retirements at 0.60`)
	for i, name := range familyOrder {
		scores := familyNamed(t, replay, name)
		source := committed.Families[name]
		r.number("replayed "+name+" median", a[2*i], scores.Median)
		r.number("replayed "+name+" IQR", a[2*i+1], scores.IQR)
		// The published figure and the replay must be the same number, or
		// the pair set is not the pair set.
		assert.InDelta(t, source.Median, scores.Median, 5e-4,
			"%s: the replay does not reproduce memory-creation's median", name)
		assert.InDelta(t, source.IQR, scores.IQR, 5e-4,
			"%s: the replay does not reproduce memory-creation's IQR", name)
	}
	correct, wrong := committed.at(t, 0.60)
	r.number("replayed correct retirements at 0.60", a[8], float64(replay.ShippedThreshold.CorrectRetirements))
	r.number("replayed wrong retirements at 0.60", a[9], float64(replay.ShippedThreshold.WrongRetirements))
	assert.Equal(t, correct, replay.ShippedThreshold.CorrectRetirements)
	assert.Equal(t, wrong, replay.ShippedThreshold.WrongRetirements)

	live, committed1536 := runs[embedderOpenAI], loadRound4(t, "separability_dims1536")
	a = r.anchor(`(\d+) wrong retirements at 0.70 against the record's (\d+), out of (\d+) pairs that must survive`)
	r.number("live wrong retirements at 0.70", a[0], float64(live.ADRThreshold.WrongRetirements))
	r.number("memory-creation's wrong retirements at 0.70", a[1], float64(secondOf(committed1536.at(t, 0.70))))
	r.number("pairs that must survive", a[2], float64(3*live.PairsPerFamily))

	a = r.anchor(`(\w+) pairs sit within 0.005 of that threshold`)
	r.number("live pairs near the ADR threshold", numberWord(t, a[0]), float64(nearThreshold(live, 0.70, 0.005)))
	assert.Zero(t, nearThreshold(runs[embedderHashing], 0.70, 0.005),
		"the record says the hashing run has no pair within 0.005 of the ADR threshold")
	r.report()
}

// nearThreshold counts pairs whose cosine sits within epsilon of a
// threshold, which is how many a rerun could move across it.
func nearThreshold(result Separability, threshold, epsilon float64) int {
	var n int
	for _, score := range result.Scores {
		if math.Abs(score.Cosine-threshold) < epsilon {
			n++
		}
	}
	return n
}

func secondOf(_, second int) int { return second }
