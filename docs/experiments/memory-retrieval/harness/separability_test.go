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

	// The shipped store retires nothing on insert: supersession on insert
	// was removed in #529, so the Superseded column reports what the
	// store does now rather than what the mechanism did. What it did is
	// in the committed results this record was written from — those
	// figures were measured while it existed and none of them moves.
	for _, score := range result.Scores {
		assert.False(t, score.Superseded,
			"%s: an insert retires nothing since #529 (cosine %v)", score.Family, score.Cosine)
	}
	for _, family := range result.Families {
		assert.Zero(t, family.Superseded, "%s: no family supersedes", family.Name)
	}

	// The cosine column is the harness's own and is unaffected: it still
	// separates the families the same way, which is the measurement the
	// removal rests on.
	unrelated := familyNamed(t, result, "unrelated")
	assert.Less(t, unrelated.Median, memory.SupersessionSimilarity)
	assert.Greater(t, familyNamed(t, result, "coexist").Median, memory.SupersessionSimilarity,
		"coexisting facts still score above the threshold that used to retire them")
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
