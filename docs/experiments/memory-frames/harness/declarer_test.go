package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What the model is shown for an assertion is what the address held before
// that write, which is what a writeback with a working recall would see.
func TestAsked_ShowsWhatTheAddressHeldBeforeTheWrite(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)

	byThread := map[string][]Declaration{}
	for _, one := range Asked(w) {
		key := one.Subject + "|" + one.Attribute
		byThread[key] = append(byThread[key], one)
	}
	for _, thread := range w.Threads {
		asked := byThread[thread.Subject+"|"+thread.Attribute]
		require.Len(t, asked, len(thread.Asserts))
		assert.Empty(t, asked[0].Held, "the first write to an address finds nothing there")
		for i, a := range thread.Asserts {
			assert.Equal(t, a.Value, asked[i].Value)
			assert.Equal(t, a.Wording, asked[i].Wording)
			want := OperationJoin
			if a.Replaces {
				want = OperationReplace
			}
			assert.Equal(t, want, asked[i].Truth)
		}
		if thread.Class == classParallel || thread.Class == classRefined {
			assert.Len(t, asked[1].Held, 1, "a coexisting value joins one already held")
		}
		if thread.Class == classReplaced {
			assert.Equal(t, []string{thread.Asserts[1].Value}, asked[2].Held,
				"a replacement leaves only itself for the next write to find")
		}
	}
}

// The prompt states the address, what is held and the sentence, and never
// the answer.
func TestDeclarePrompt_NamesNeitherOperation(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)

	for _, one := range Asked(w) {
		prompt := declarePrompt(one)
		assert.Contains(t, prompt, one.Subject)
		assert.Contains(t, prompt, one.Attribute)
		assert.Contains(t, prompt, one.Text)
		assert.NotContains(t, prompt, string(OperationReplace))
		assert.NotContains(t, prompt, string(OperationJoin))
	}
}

func TestParseOperation_KeepsAnUnusableAnswerAsWrong(t *testing.T) {
	tests := []struct {
		answer string
		want   Operation
	}{
		{"replace", OperationReplace},
		{"  Join.", OperationJoin},
		{"REPLACE", OperationReplace},
		{"it depends", Operation("unusable")},
		{"", Operation("unusable")},
	}
	for _, tc := range tests {
		t.Run(tc.answer, func(t *testing.T) {
			assert.Equal(t, tc.want, parseOperation(tc.answer))
		})
	}
}

// scored builds a declaration set by hand so the scoring rules are checked
// against answers whose every direction and denominator is known.
func scored() Declarations {
	return Declarations{Model: "test", Declarations: []Declaration{
		// An opening write: replacing and joining are the same store, so
		// its declaration has no consequence either way.
		{Subject: "Alma", Attribute: "project", Value: "a", Year: 1, Wording: wordingAssert,
			Class: classParallel, Truth: OperationReplace, Declared: OperationJoin},
		// A join answered right, and a join answered as a replacement:
		// the destroying direction.
		{Subject: "Alma", Attribute: "project", Value: "b", Year: 6, Wording: wordingAlso,
			Class: classParallel, Held: []string{"a"}, Truth: OperationJoin, Declared: OperationJoin},
		{Subject: "Otto", Attribute: "clinic", Value: "d", Year: 6, Wording: wordingNarrow,
			Class: classRefined, Held: []string{"c"}, Truth: OperationJoin, Declared: OperationReplace},
		// A replacement answered as a join: the accumulating direction.
		{Subject: "Iris", Attribute: "line", Value: "f", Year: 3, Wording: wordingReplace,
			Class: classReplaced, Held: []string{"e"}, Truth: OperationReplace, Declared: OperationJoin},
	}}
}

func TestScoreDeclarations_ReportsEachDirectionAgainstItsOwnDenominator(t *testing.T) {
	score := ScoreDeclarations(scored())

	assert.Equal(t, 4, score.Asked)
	assert.Equal(t, 3, score.Consequential)
	assert.Equal(t, 0.25, score.Accuracy.Mean, "one of four answers matched the planted operation")
	assert.InDelta(t, 1.0/3.0, score.ConsequentialAcc.Mean, 1e-9)

	byDirection := map[string]DirectionScore{}
	for _, d := range score.Directions {
		byDirection[d.Direction] = d
	}
	destroy := byDirection[directionJoinAsReplace]
	assert.Equal(t, 2, destroy.Eligible)
	assert.Equal(t, 1, destroy.Errors)
	assert.Equal(t, 2, destroy.Consequential, "a join always lands on an address holding something")

	accumulate := byDirection[directionReplaceAsJoin]
	assert.Equal(t, 2, accumulate.Eligible)
	assert.Equal(t, 2, accumulate.Errors)
	assert.Equal(t, 1, accumulate.Consequential,
		"the opening write is eligible and cannot be wrong, so the two denominators differ")
	assert.Equal(t, 1.0, accumulate.Rate.Mean)
	assert.Equal(t, 1.0, accumulate.ConsequentialRate.Mean)
}

func TestScoreDeclarations_CutsByWordingAndByClass(t *testing.T) {
	score := ScoreDeclarations(scored())

	byWording := map[string]CutScore{}
	for _, c := range score.Wordings {
		byWording[c.Name] = c
	}
	assert.Equal(t, 1.0, byWording[wordingAlso].Accuracy.Mean)
	assert.Equal(t, 0.0, byWording[wordingNarrow].Accuracy.Mean)
	assert.Equal(t, 0.0, byWording[wordingAssert].Accuracy.Mean)

	byClass := map[string]CutScore{}
	for _, c := range score.Classes {
		byClass[c.Name] = c
	}
	assert.Equal(t, 0.5, byClass[classParallel].Accuracy.Mean)
	assert.Equal(t, 0.0, byClass[classRefined].Accuracy.Mean)
}

func TestDeclarations_FlipsAreTheWrongAnswersInOneDirection(t *testing.T) {
	d := scored()

	destroy := d.Flips(directionJoinAsReplace)
	require.Len(t, destroy, 1)
	assert.Equal(t, Address{Subject: "Otto", Attribute: "clinic"}, destroy[0].Address)
	assert.Equal(t, 6, destroy[0].Year)

	accumulate := d.Flips(directionReplaceAsJoin)
	require.Len(t, accumulate, 2)
}

// The substitution finds an assertion by its address, its value and the
// year it was said in, so a thread that returns to a value gets the answer
// given for that write rather than for the earlier one.
func TestDeclarations_DeclaringSubstitutesTheAnswerForThatWrite(t *testing.T) {
	d := Declarations{Declarations: []Declaration{
		{Subject: "Alma", Attribute: "project", Value: "a", Year: 1, Truth: OperationReplace,
			Declared: OperationJoin},
		{Subject: "Alma", Attribute: "project", Value: "a", Year: 7, Truth: OperationReplace,
			Declared: OperationReplace},
	}}
	declaring := d.Declaring()

	early := wrote(0, almaProject, "a", "first.")
	early.Declared = OperationReplace
	assert.Equal(t, OperationJoin, declaring(early).Declared)

	late := wrote(6*daysPerYear+10, almaProject, "a", "back again.")
	late.Declared = OperationReplace
	assert.Equal(t, OperationReplace, declaring(late).Declared)

	// A turn the model was never asked about keeps what the workload said.
	unknown := wrote(0, almaProject, "z", "never asked.")
	unknown.Declared = OperationReplace
	assert.Equal(t, OperationReplace, declaring(unknown).Declared)
}

func TestDeclareCost_IsTheArithmeticTheRecordReports(t *testing.T) {
	assert.InDelta(t, declarePriceIn, declareCost(1_000_000, 0), 1e-12)
	assert.InDelta(t, declarePriceOut, declareCost(0, 1_000_000), 1e-12)
	assert.Zero(t, declareCost(0, 0))
}
