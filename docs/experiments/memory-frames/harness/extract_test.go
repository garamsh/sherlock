package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// planted is the one assertion the scoring tests are read against: a value
// no other thread carries, at an address the model either finds or does
// not.
var planted = Written{
	Fact:     Fact{Address: Address{Subject: "Wilma", Attribute: "supplier"}, Value: "larkin-weir"},
	Declared: OperationReplace,
}

// plantedWorkload is the workload the score is cut by. ScoreExtraction
// reads the wording and the class off the threads and never off the
// model's answer, so one thread carrying the planted value is enough.
func plantedWorkload() Workload {
	return Workload{Threads: []Thread{{
		Class: classFresh, Subject: planted.Subject, Attribute: planted.Attribute,
		Asserts: []Assert{{Year: 1, Value: planted.Value, Wording: wordingAssert, Replaces: true}},
	}}}
}

// readAs is one message the model read, carrying the planted assertion and
// whatever it returned for it.
func readAs(found ...Written) Extraction {
	return Extraction{Shape: shapePacked, Model: declareModel,
		Messages: []Extracted{{Index: 0, Text: "a message", Planted: []Written{planted}, Found: found}}}
}

// elsewhere is the planted value returned at another attribute of the same
// subject: the failure that puts something where nothing belongs.
var elsewhere = Written{
	Fact:     Fact{Address: Address{Subject: planted.Subject, Attribute: "accounts"}, Value: planted.Value},
	Declared: planted.Declared,
}

func TestScoreExtractionSeparatesTheThreeFailures(t *testing.T) {
	tests := []struct {
		name                                             string
		found                                            []Written
		correct, missed, misaddressed, invented, planted int
	}{
		{name: "read where it was written", found: []Written{planted}, correct: 1, planted: 1},
		{name: "read at the wrong address", found: []Written{elsewhere}, misaddressed: 1, planted: 1},
		{name: "not read at all", found: nil, missed: 1, planted: 1},
		{name: "read and something else invented", found: []Written{planted,
			{Fact: Fact{Address: planted.Address, Value: "never-asserted"}}},
			correct: 1, invented: 1, planted: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			score := ScoreExtraction(plantedWorkload(), readAs(test.found...))

			assert.Equal(t, test.planted, score.Planted)
			assert.Equal(t, test.correct, score.Correct)
			assert.Equal(t, test.missed, score.Missed,
				"a fact put somewhere else was found, so it is not missing")
			assert.Equal(t, test.misaddressed, score.Misaddressed,
				"a fact at the wrong address corrupts a store rather than leaving it empty")
			assert.Equal(t, test.invented, score.Invented)
		})
	}
}

func TestScoreExtractionCountsAnInventionAgainstTheMessage(t *testing.T) {
	invented := Written{Fact: Fact{Address: Address{Subject: "Wilma", Attribute: "supplier"},
		Value: "never-asserted"}}
	e := Extraction{Messages: []Extracted{{Index: 0, Text: "small talk", Found: []Written{invented}}}}

	score := ScoreExtraction(plantedWorkload(), e)

	assert.Zero(t, score.Planted, "a message asserting nothing plants nothing")
	assert.Equal(t, 1, score.Invented)
	assert.Equal(t, float64(1), score.InventedPerMessage.Mean,
		"counting inventions against planted facts would hide the message that planted none")
}

func TestScoreExtractionReadsTheOperationOnlyWhereTheFactWasRead(t *testing.T) {
	wrongOperation := planted
	wrongOperation.Declared = OperationJoin

	read := ScoreExtraction(plantedWorkload(), readAs(wrongOperation))
	assert.Equal(t, 1, read.Correct)
	assert.Zero(t, read.DeclaredCorrect)
	assert.Equal(t, float64(0), read.DeclaredRate.Mean)

	misread := ScoreExtraction(plantedWorkload(), readAs(elsewhere))
	assert.Zero(t, misread.DeclaredRate.N,
		"an operation on a fact put in the wrong place answers no question this record asks")
}

func TestScoreExtractionCutsByTheWorkloadsWordingAndClass(t *testing.T) {
	score := ScoreExtraction(plantedWorkload(), readAs(planted))

	require.Len(t, score.Wordings, 1)
	assert.Equal(t, wordingAssert, score.Wordings[0].Name)
	assert.Equal(t, 1, score.Wordings[0].Asked)
	require.Len(t, score.Classes, 1)
	assert.Equal(t, classFresh, score.Classes[0].Name)
}

func TestParseExtractionReadsTheAnswer(t *testing.T) {
	tests := []struct {
		name   string
		answer string
		want   []Written
	}{
		{name: "an answer that will not parse reads as nothing found", answer: "sorry, no JSON here"},
		{name: "a message asserting nothing", answer: `{"facts":[]}`, want: []Written{}},
		{name: "a fact missing a field is dropped",
			answer: `{"facts":[{"subject":"Wilma","attribute":"supplier","operation":"replace"}]}`,
			want:   []Written{}},
		{name: "an attribute and a value are lowered, a subject is not",
			answer: `{"facts":[{"subject":"Wilma","attribute":"Supplier","value":"Larkin-Weir","operation":"replace"}]}`,
			want:   []Written{planted}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, parseExtraction(test.answer))
		})
	}
}

func TestObservedCarriesWhatTheModelReadAndNotWhatWasPlanted(t *testing.T) {
	w := Workload{Turns: []Turn{
		{Index: 0, Text: "a message", Written: []Written{planted}},
		{Index: 1, Text: "small talk"},
	}}

	observed := Extraction{Messages: []Extracted{
		{Index: 0, Text: "a message", Planted: []Written{planted}, Found: []Written{elsewhere}},
	}}.Observed(w)

	require.Len(t, observed.Turns, 2)
	assert.Equal(t, []Written{elsewhere}, observed.Turns[0].Written,
		"the extracted arm delivers what a model read")
	assert.Empty(t, observed.Turns[1].Written, "a message the extraction says nothing about carries nothing")
	assert.Equal(t, []Written{planted}, w.Turns[0].Written, "the workload it was built from is unchanged")
}
