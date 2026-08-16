package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateCorpus_IsDeterministicInTheSeed(t *testing.T) {
	first, err := GenerateCorpus(7, 20, 400, 200)
	require.NoError(t, err)
	second, err := GenerateCorpus(7, 20, 400, 200)
	require.NoError(t, err)
	other, err := GenerateCorpus(11, 20, 400, 200)
	require.NoError(t, err)

	assert.Equal(t, first, second)
	assert.NotEqual(t, first.Items, other.Items)
}

func TestGenerateCorpus_PlantsEveryAnswerInTheSmallestStore(t *testing.T) {
	const head = 200
	corpus, err := GenerateCorpus(7, 20, 400, head)
	require.NoError(t, err)

	for _, probe := range corpus.Probes {
		var inHead, total int
		for i, it := range corpus.Items {
			if !probe.Answers(it) {
				continue
			}
			total++
			if i < head {
				inHead++
			}
		}
		require.GreaterOrEqual(t, total, minFacts, "probe %q", probe.Task)
		require.LessOrEqual(t, total, maxFacts, "probe %q", probe.Task)
		assert.Equal(t, total, inHead, "probe %q has answers outside the smallest store", probe.Task)
	}
}

func TestGenerateCorpus_PlantsTheRankingInputsIndependentlyOfTheAnswer(t *testing.T) {
	corpus, err := GenerateCorpus(7, 20, 400, 200)
	require.NoError(t, err)

	answers := map[string]bool{}
	for _, probe := range corpus.Probes {
		answers[probe.Subject+"/"+probe.Topic] = true
	}
	salience := map[bool]map[float64]int{true: {}, false: {}}
	for _, it := range corpus.Items {
		require.Contains(t, []float64{salienceAddEpisodic, salienceAddSemantic, salienceUpdate}, it.Salience)
		require.Less(t, it.AgeHours, ageWindowHours)
		salience[answers[it.Subject+"/"+it.Topic]][it.Salience]++
	}
	// Every salience the platform derives appears on both sides of the
	// answer boundary, so the ranking's salience term carries no signal
	// about which memories answer a probe.
	for _, tier := range []float64{salienceAddEpisodic, salienceAddSemantic, salienceUpdate} {
		assert.Positive(t, salience[true][tier], "no answer carries salience %v", tier)
		assert.Positive(t, salience[false][tier], "no distractor carries salience %v", tier)
	}
}

// A probe is answered by the pair the generator planted, never by what a
// fact's words look like. The corpus carries values that contain other
// values as substrings, so the two rules disagree by construction.
func TestProbeAnswers_RejectsASubstringOfAnotherFact(t *testing.T) {
	probe := Probe{Task: "Plan a dinner for Alma.", Subject: "Alma", Topic: "allergy"}
	planted := Item{Content: "Alma is allergic to fish.", Subject: "Alma", Topic: "allergy"}
	otherSubject := Item{Content: "Otto is allergic to shellfish.", Subject: "Otto", Topic: "allergy"}
	otherTopic := Item{Content: "Alma cooks fish at the weekend.", Subject: "Alma", Topic: "cooking"}

	assert.True(t, probe.Answers(planted))
	assert.False(t, probe.Answers(otherSubject))
	assert.False(t, probe.Answers(otherTopic))

	// What a substring rule would have done with the same three facts.
	for _, it := range []Item{planted, otherSubject, otherTopic} {
		assert.True(t, strings.Contains(strings.ToLower(it.Content), "fish"),
			"the substring trap needs all three facts to contain the value")
	}
}

func TestCorpusArms_ChangeOnlyWhatTheRankingReadsBesidesRelevance(t *testing.T) {
	corpus, err := GenerateCorpus(7, 20, 400, 200)
	require.NoError(t, err)

	aged := corpus.WithAgeScale(12)
	flat := corpus.Flat()
	require.Len(t, aged.Items, len(corpus.Items))
	require.Len(t, flat.Items, len(corpus.Items))

	for i, it := range corpus.Items {
		assert.Equal(t, it.Content, aged.Items[i].Content)
		assert.Equal(t, it.Content, flat.Items[i].Content)
		assert.Equal(t, it.AgeHours*12, aged.Items[i].AgeHours)
		assert.Equal(t, it.Salience, aged.Items[i].Salience)
		assert.Zero(t, flat.Items[i].AgeHours)
		assert.Equal(t, salienceAddSemantic, flat.Items[i].Salience)
	}
	assert.Equal(t, corpus.Probes, flat.Probes)
}

func TestGenerateCorpus_RejectsACorpusItCannotBuild(t *testing.T) {
	_, err := GenerateCorpus(7, 0, 400, 200)
	assert.Error(t, err)

	_, err = GenerateCorpus(7, len(subjects)*len(topics)+1, 400, 200)
	assert.Error(t, err)

	// A head too small to hold every probed fact would leave a probe
	// unanswerable at the smallest store size.
	_, err = GenerateCorpus(7, 20, 400, 10)
	assert.Error(t, err)
}
