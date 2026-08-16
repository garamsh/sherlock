package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every question a frame is asked has to be one the resolver was asked
// about, or the resolved form would be measuring which questions were
// bought rather than what a model made of them.
func TestQuestions_CoverEveryQuestionTheFindingRunAsks(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)

	asked := map[string]bool{}
	for _, question := range Questions(w) {
		assert.False(t, asked[question.Text], "%q is asked about twice", question.Text)
		asked[question.Text] = true
	}
	for _, thread := range w.Threads {
		assert.True(t, asked[thread.Question], "no resolution covers %q", thread.Question)
		assert.True(t, asked[subjectQuestion(thread.Subject)],
			"no resolution covers the question about %s", thread.Subject)
	}
}

// The three ways a resolution fails are different harms: a question placed
// on another subject is answered out of someone else's addresses, one
// placed on the wrong attribute is answered out of the right person's wrong
// one, and one the model cannot place reaches the store as nothing.
func TestScoreResolutions_SeparatesTheWaysAModelFails(t *testing.T) {
	answered := Resolutions{Model: "test", Resolutions: []Resolution{
		{Question: "a", Kind: kindThread, Subject: "Alma", Attribute: "project",
			FoundSubject: "Alma", FoundAttribute: "project"},
		{Question: "b", Kind: kindThread, Subject: "Alma", Attribute: "clinic",
			FoundSubject: "Otto", FoundAttribute: "clinic"},
		{Question: "c", Kind: kindThread, Subject: "Alma", Attribute: "line",
			FoundSubject: "Alma", FoundAttribute: "bank"},
		{Question: "d", Kind: kindSubject, Subject: "Alma"},
	}}

	score := ScoreResolutions(answered)

	assert.Equal(t, 4, score.Asked)
	assert.Equal(t, 1, score.Correct)
	assert.Equal(t, 1, score.WrongSubject)
	assert.Equal(t, 1, score.WrongAttribute)
	assert.Equal(t, 1, score.Unplaced)
	assert.Equal(t, 0.25, score.CorrectRate.Mean)
	require.Len(t, score.Kinds, 2)
	assert.Equal(t, kindThread, score.Kinds[0].Name)
	assert.Equal(t, 3, score.Kinds[0].Asked)
}

// A question the model could not place resolves to no address at all,
// rather than to some address the run chose on its behalf.
func TestResolving_LeavesAQuestionUnplacedWhereTheModelDid(t *testing.T) {
	resolving := Resolutions{Resolutions: []Resolution{
		{Question: "Which project does Alma lead?", FoundSubject: "Alma", FoundAttribute: "project"},
		{Question: "What do you know about Otto?", FoundSubject: "Otto"},
		{Question: "Which clinic does Kofi go to?"},
	}}.Resolving()

	assert.Equal(t, &Address{Subject: "Alma", Attribute: "project"},
		resolving("Which project does Alma lead?"))
	assert.Equal(t, &Address{Subject: "Otto"}, resolving("What do you know about Otto?"))
	assert.Nil(t, resolving("Which clinic does Kofi go to?"))
	assert.Nil(t, resolving("a question nobody asked"))
}

// A model's answer is normalised the way the write side normalises what it
// reads, so what is measured is where a question was placed and not how it
// was spelled. An answer that is not a subject at all is left as it is and
// misses.
func TestWithAnswer_SpellsAnAddressTheWayTheStoreHoldsIt(t *testing.T) {
	tests := []struct {
		name            string
		answer          string
		subject, attrib string
	}{
		{"the spelling the store holds", `{"subject":"alma","attribute":"Project"}`, "Alma", "project"},
		{"a subject the store never held", `{"subject":"Nobody","attribute":"project"}`, "Nobody", "project"},
		{"a whole-person question", `{"subject":"Alma","attribute":""}`, "Alma", ""},
		{"an answer that will not parse", `not json`, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolved := withAnswer(Resolution{Question: "q"}, tc.answer)
			assert.Equal(t, tc.subject, resolved.FoundSubject)
			assert.Equal(t, tc.attrib, resolved.FoundAttribute)
		})
	}
}
