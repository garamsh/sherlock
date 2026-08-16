package main

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// trapCorpus holds one planted answer and two facts a content match would
// confuse with it: the same value under another subject, and the same
// subject under another topic.
func trapCorpus() Corpus {
	return Corpus{
		Seed: 1,
		Probes: []Probe{{
			Task:    "Plan a dinner for Alma that leaves out everything they are allergic to.",
			Subject: "Alma",
			Topic:   "allergy",
		}},
		Items: []Item{
			{Content: "Alma is allergic to fish.", Subject: "Alma", Topic: "allergy",
				Type: "semantic", Salience: salienceAddSemantic},
			{Content: "Otto is allergic to shellfish.", Subject: "Otto", Topic: "allergy",
				Type: "semantic", Salience: salienceAddSemantic},
			{Content: "Alma cooks fish at the weekend.", Subject: "Alma", Topic: "cooking",
				Type: "semantic", Salience: salienceAddSemantic},
		},
	}
}

// The whole store is returned, so every credit the rule gives is visible:
// one hit out of three returned memories. A rule that matched the probe's
// value against content would credit all three.
func TestMeasure_CreditsOnlyThePlantedAnswer(t *testing.T) {
	corpus := trapCorpus()
	result, err := Measure(context.Background(), corpus, len(corpus.Items), now)
	require.NoError(t, err)
	require.Len(t, result.Probes, 1)

	probe := result.Probes[0]
	assert.Equal(t, 1, probe.Relevant)
	assert.Equal(t, len(corpus.Items), probe.ReturnedAtLimit)
	assert.Equal(t, 1, probe.HitsAtLimit)
	assert.Equal(t, 1, result.RelevantPlanted)

	var substringCredits int
	for _, it := range corpus.Items {
		if strings.Contains(strings.ToLower(it.Content), "fish") {
			substringCredits++
		}
	}
	assert.Equal(t, len(corpus.Items), substringCredits,
		"the trap only holds if a substring rule credits every fact")
}

func TestMeasure_RanksThePlantedAnswerFirstWhenNothingElseIsRelevant(t *testing.T) {
	corpus := trapCorpus()
	result, err := Measure(context.Background(), corpus, len(corpus.Items), now)
	require.NoError(t, err)

	probe := result.Probes[0]
	assert.Equal(t, 1, probe.BestRank)
	assert.Equal(t, 1, probe.HitsAt1)
	assert.Equal(t, 1.0, result.Figures.RecallAtLimit.Mean)
	assert.InDelta(t, 1.0/3.0, result.Figures.PrecisionAtLimit.Mean, 1e-9)
}

func TestMeasure_ReportsWhatTheShippedRecallBudgetReturns(t *testing.T) {
	corpus := trapCorpus()
	result, err := Measure(context.Background(), corpus, len(corpus.Items), now)
	require.NoError(t, err)

	probe := result.Probes[0]
	assert.Positive(t, probe.ReturnedAtBudget)
	assert.LessOrEqual(t, probe.ReturnedAtBudget, probe.ReturnedAtLimit)
	assert.LessOrEqual(t, probe.HitsAtBudget, probe.HitsAtLimit)
}

func TestMeasure_RejectsAProbeWithNoPlantedAnswer(t *testing.T) {
	corpus := trapCorpus()
	corpus.Probes = append(corpus.Probes, Probe{Task: "Ask Otto about nothing.", Subject: "Otto", Topic: "degree"})
	_, err := Measure(context.Background(), corpus, len(corpus.Items), now)
	assert.Error(t, err)

	_, err = Measure(context.Background(), corpus, len(corpus.Items)+1, now)
	assert.Error(t, err)
}
