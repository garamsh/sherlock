package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/memory"
)

// trapPairSet holds one pair the shipped store must retire and three it
// must not, with the same shape as the committed set.
func trapPairSet() PairSet {
	return PairSet{
		Seed:          1,
		MustSupersede: map[string]bool{"correction": true, "coexist": false, "cross-subject": false, "unrelated": false},
		Families: map[string][]Pair{
			"correction": {{
				Old: "Alma is allergic to fish.",
				New: "Alma is allergic to shellfish.",
			}},
			"coexist": {{
				Old: "Bruno speaks korean.",
				New: "Bruno speaks german.",
			}},
			"cross-subject": {{
				Old: "Otto plays the cello.",
				New: "Maria plays the cello.",
			}},
			"unrelated": {{
				Old: "Lisa lived in lisbon.",
				New: "Kofi trains judo twice a week.",
			}},
		},
	}
}

func TestMeasureSeparability_ReportsWhatTheShippedStoreDid(t *testing.T) {
	result, err := MeasureSeparability(context.Background(), trapPairSet(), hashingEmbedder(t), now)
	require.NoError(t, err)

	require.Len(t, result.Families, 4)
	assert.Equal(t, memory.SupersessionSimilarity, result.SupersessionSimilarity)
	assert.Equal(t, memory.EmbeddingDim, result.Dimensions)
	assert.Equal(t, embedderHashing, result.Embedder)
	assert.Equal(t, 4, result.PairsMeasured)

	// The store's own decision and the harness cosine must agree on every
	// pair, or the sweep is sweeping a number the mechanism does not read.
	assert.Equal(t, result.PairsMeasured, result.StoreAgreesWithCosine)
	for _, family := range result.Families {
		for _, score := range result.Scores {
			if score.Family != family.Name {
				continue
			}
			assert.Equal(t, score.Cosine >= memory.SupersessionSimilarity, score.Superseded,
				"%s: cosine %v and the store disagree", family.Name, score.Cosine)
		}
	}

	unrelated := familyNamed(t, result, "unrelated")
	assert.Zero(t, unrelated.Superseded, "unrelated statements must not supersede")
	assert.Less(t, unrelated.Median, memory.SupersessionSimilarity)
}

func TestMeasureSeparability_SweepsEveryThreshold(t *testing.T) {
	result, err := MeasureSeparability(context.Background(), trapPairSet(), hashingEmbedder(t), now)
	require.NoError(t, err)

	require.Len(t, result.Sweep, 101)
	assert.InDelta(t, 0, result.Sweep[0].Threshold, 1e-9)
	assert.InDelta(t, 1, result.Sweep[100].Threshold, 1e-9)
	// At a threshold of zero everything supersedes: every correction is
	// retired and so is every pair that had to survive.
	assert.Equal(t, 1, result.Sweep[0].CorrectRetirements)
	assert.Equal(t, 3, result.Sweep[0].WrongRetirements)
	// Above one nothing does.
	assert.Zero(t, result.Sweep[100].CorrectRetirements)
	assert.Zero(t, result.Sweep[100].WrongRetirements)
	assert.GreaterOrEqual(t, result.BestThreshold.Accuracy, result.ShippedThreshold.Accuracy)
	assert.InDelta(t, 0.70, result.ADRThreshold.Threshold, 1e-9)
}

func TestMeasureSeparability_RejectsAPairSetMissingAFamily(t *testing.T) {
	set := trapPairSet()
	delete(set.Families, "coexist")
	_, err := MeasureSeparability(context.Background(), set, hashingEmbedder(t), now)
	assert.Error(t, err)
}

func TestCommittedPairSet_IsTheRound4SetAndNothingElse(t *testing.T) {
	set, err := LoadPairSet(filepath.Join(resultsDir, "separability_pairs_seed7.json"))
	require.NoError(t, err)

	assert.Equal(t, 7, set.Seed)
	assert.Contains(t, set.Source, "round4/separability.py")
	require.Len(t, set.Families, 4)
	for _, name := range familyOrder {
		assert.Len(t, set.Families[name], 250, "family %s", name)
	}
	// Only corrections may be retired; the other three families are what a
	// threshold must leave alone.
	assert.Equal(t, map[string]bool{"correction": true, "coexist": false,
		"cross-subject": false, "unrelated": false}, set.MustSupersede)

	// No pair may state the same thing twice: a pair that did would
	// supersede under any embedder and measure nothing.
	for name, pairs := range set.Families {
		for i, pair := range pairs {
			assert.NotEqual(t, pair.Old, pair.New, "%s pair %d repeats itself", name, i)
		}
	}
}

func familyNamed(t *testing.T, result Separability, name string) FamilyScores {
	t.Helper()
	for _, family := range result.Families {
		if family.Name == name {
			return family
		}
	}
	t.Fatalf("no %q family in the result", name)
	return FamilyScores{}
}
