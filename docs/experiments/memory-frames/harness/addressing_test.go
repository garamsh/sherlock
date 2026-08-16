package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAskedToAddress_AsksAboutEveryAssertionTheDeclaringPassAsksAbout(t *testing.T) {
	w, err := GenerateWorkload(7, decadeParams())
	require.NoError(t, err)

	assert.Len(t, AskedToAddress(w), len(Asked(w)),
		"the two paid passes have to ask about one population to be read against each other")
}

func TestAskedToAddress_CarriesTheRuleSAddressAndNoAnswer(t *testing.T) {
	w, err := GenerateWorkload(7, decadeParams())
	require.NoError(t, err)

	for _, one := range AskedToAddress(w) {
		require.NotEmpty(t, one.Formed.Attribute)
		assert.Empty(t, one.Answered.Attribute, "an unasked assertion carries no answer")
	}
}

func TestParseAddress_ReadsAnAnswerAndSurvivesOneItCannotRead(t *testing.T) {
	tests := []struct {
		name   string
		answer string
		want   Address
	}{
		{"a question and a restriction",
			`{"question":"Which project does lead","restriction":"On Thursdays"}`,
			Address{Subject: "Maria", Attribute: "which project does lead", Qualifier: "on thursdays"}},
		{"a restriction with trailing punctuation",
			`{"question":"which-project-does-lead","restriction":"for payroll."}`,
			Address{Subject: "Maria", Attribute: "which-project-does-lead", Qualifier: "for payroll"}},
		{"no restriction",
			`{"question":"which-project-does-lead","restriction":""}`,
			Address{Subject: "Maria", Attribute: "which-project-does-lead"}},
		{"an answer that will not parse", "sorry, I cannot", Address{Subject: "Maria"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, parseAddress(test.answer, "Maria"))
		})
	}
}

func TestScoreAddresses_ReportsAWrongQuestionApartFromAWrongRestriction(t *testing.T) {
	formed := Address{Subject: "Maria", Attribute: "which-project-does-lead"}
	restricted := Address{Subject: "Maria", Attribute: "which-project-does-lead", Qualifier: "on thursdays"}
	scored := ScoreAddresses(Addresses{Addressed: []Addressed{
		{Wording: wordingAssert, Class: classCarried, Formed: formed, Answered: formed},
		{Wording: wordingReplace, Class: classReplaced, Formed: formed,
			Answered: Address{Subject: "Maria", Attribute: "which-project-does-lead", Qualifier: "now"}},
		{Wording: wordingNarrow, Class: classRefined, Formed: restricted,
			Answered: Address{Subject: "Maria", Attribute: "which-project-does-lead", Qualifier: "thursdays"}},
		{Wording: wordingAlso, Class: classParallel, Formed: formed,
			Answered: Address{Subject: "Maria", Attribute: "where-does-rehearse"}},
	}})

	assert.Equal(t, 1, scored.Right, "one of the four is the rule's address exactly")
	assert.InDelta(t, 0.75, scored.QuestionAcc.Mean, 1e-9, "one of the four names another question")
	assert.Equal(t, 1, scored.Restricted, "the rule restricts one of the four")
	assert.InDelta(t, 0.0, scored.RestrictionAcc.Mean, 1e-9, "and the model worded that one differently")
	assert.InDelta(t, 1.0, scored.RestrictionOverlap.Mean, 1e-9, "which is the same phrase, not another one")
	assert.Equal(t, 1, scored.Invented, "a restriction where the rule reads none")
}

func TestSharePhrase_SeparatesTheSamePhraseFromAnotherOne(t *testing.T) {
	tests := []struct {
		name             string
		answered, formed string
		want             bool
	}{
		{"the same words", "on thursdays", "on thursdays", true},
		{"the same phrase without its preposition", "thursdays", "on thursdays", true},
		{"another phrase", "now", "on thursdays", false},
		{"nothing answered", "", "on thursdays", false},
		{"nothing formed", "now", "", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, sharePhrase(test.answered, test.formed))
		})
	}
}
