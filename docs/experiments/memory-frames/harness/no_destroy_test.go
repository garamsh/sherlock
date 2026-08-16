package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// declaredWrite is a write that states what it does to the address.
func declaredWrite(day int, address Address, value, text string, op Operation) Turn {
	t := wrote(day, address, value, text)
	t.Declared = op
	return t
}

// The round's central measurement, at the smallest scale that shows it: a
// wrongly-declared replacement destroys the value in an addressed store and
// only closes its span in an interval store — and the present-tense answer
// is the same either way, because a closed span is not what is true now.
func TestIntervalDeclared_KeepsWhatWasClearedAndStillCannotAnswerWithIt(t *testing.T) {
	ctx := context.Background()
	addressed := NewAttributeFrame(arityDeclared)
	spans := NewIntervalFrame(arityDeclared)

	// A value, then a second that is true at the same time but arrives
	// declared as a replacement: the destroying error.
	for _, frame := range []Frame{addressed, spans} {
		require.NoError(t, frame.Observe(ctx,
			declaredWrite(0, almaProject, "amber-quarry", "first.", OperationReplace)))
		require.NoError(t, frame.Observe(ctx,
			declaredWrite(400, almaProject, "copper-lantern", "and also.", OperationReplace)))
	}

	fromAddressed, err := addressed.Context(ctx, asked(800, almaProject))
	require.NoError(t, err)
	fromSpans, err := spans.Context(ctx, asked(800, almaProject))
	require.NoError(t, err)

	assert.Equal(t, fromAddressed.Text, fromSpans.Text,
		"holding the cleared value changes nothing about the present-tense answer")
	assert.False(t, Mentions(fromSpans.Text, "amber-quarry"),
		"a closed span is not what is true now")

	// The information is not gone, which is the whole difference.
	held := spans.(*intervalFrame).Values(almaProject)
	assert.Contains(t, held, "amber-quarry", "the store still holds what the write cleared")
	assert.Contains(t, held, "copper-lantern")

	addressedSize, err := addressed.Size(ctx)
	require.NoError(t, err)
	spansSize, err := spans.Size(ctx)
	require.NoError(t, err)
	assert.Greater(t, spansSize.Units, addressedSize.Units, "and pays for holding it")

	// Reaching it takes a different question, which the workload never asks.
	earlier, err := spans.Context(ctx, asked(200, almaProject))
	require.NoError(t, err)
	assert.True(t, Mentions(earlier.Text, "amber-quarry"),
		"asked as of a moment inside the closed span, the value comes back")
}

// A write that declares a join destroys nothing in either store, so the two
// only diverge in what they hold where a declaration was wrong.
func TestIntervalDeclared_ClosesNothingWhenTheWriteJoins(t *testing.T) {
	ctx := context.Background()
	frame := NewIntervalFrame(arityDeclared)

	require.NoError(t, frame.Observe(ctx,
		declaredWrite(0, almaProject, "amber-quarry", "first.", OperationReplace)))
	require.NoError(t, frame.Observe(ctx,
		declaredWrite(400, almaProject, "copper-lantern", "as well.", OperationJoin)))

	injected, err := frame.Context(ctx, asked(800, almaProject))
	require.NoError(t, err)
	assert.True(t, Mentions(injected.Text, "amber-quarry"))
	assert.True(t, Mentions(injected.Text, "copper-lantern"))
}

// The bias's two ends are the two structures anyone could build: take the
// declaration, or never destroy. Everything between them is a share of
// writes chosen by nothing.
func TestJoinBias_EndsAreTakeTheDeclarationAndNeverDestroy(t *testing.T) {
	replace := declaredWrite(3, almaProject, "copper-lantern", "replaces.", OperationReplace)
	join := declaredWrite(4, almaProject, "amber-quarry", "joins.", OperationJoin)

	none := JoinBias{Share: 0, Seed: 7}.over(nil)
	assert.Equal(t, OperationReplace, none(replace).Declared)
	assert.Equal(t, OperationJoin, none(join).Declared)

	all := JoinBias{Share: 1, Seed: 7}.over(nil)
	assert.Equal(t, OperationJoin, all(replace).Declared, "no write destroys at the far end")
	assert.Equal(t, OperationJoin, all(join).Declared)
}

// Never destroying is not a new structure: it is what the store that keeps
// every value already does.
func TestJoinBias_AtTheFarEndHoldsWhatKeepingEveryValueHolds(t *testing.T) {
	ctx := context.Background()
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)
	all := JoinBias{Share: 1, Seed: 7}.over(nil)

	biased, err := Measure(ctx, w, frameAttributeDeclared, armOracle, NewEmbedCounter(), all)
	require.NoError(t, err)
	keeping, err := Measure(ctx, w, frameAttributeMulti, armOracle, NewEmbedCounter(), nil)
	require.NoError(t, err)

	assert.Equal(t, keeping.Probes, biased.Probes)
	assert.Equal(t, keeping.Sizes, biased.Sizes)
}

// Two stores taking the same declarations answer the same, whatever one of
// them additionally keeps. It is the same equality #457 found between three
// stores under one self-deciding rule, and it holds under a declaration
// that is sometimes wrong.
func TestMeasure_GivesBothDeclaredStoresTheSameAnswer(t *testing.T) {
	ctx := context.Background()
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)

	answers := func(name string) ([]ProbeResult, []SizeAt, float64) {
		measured, err := Measure(ctx, w, name, armOracle, NewEmbedCounter(), nil)
		require.NoError(t, err)
		out := make([]ProbeResult, len(measured.Probes))
		var read float64
		for i, p := range measured.Probes {
			read += float64(p.Scanned)
			p.Scanned = 0
			out[i] = p
		}
		return out, measured.Sizes, read
	}
	addressed, addressedSizes, addressedRead := answers(frameAttributeDeclared)
	spans, spanSizes, spansRead := answers(frameIntervalDeclared)

	assert.Equal(t, addressed, spans)
	assert.NotEqual(t, addressedSizes, spanSizes, "and they do not hold the same thing")
	assert.Greater(t, spansRead, addressedRead, "nor read the same amount to answer")
}

func TestResampled_ReadsTheSamplesItWasGiven(t *testing.T) {
	tests := []struct {
		name      string
		samples   []Operation
		truth     Operation
		majority  Operation
		unanimous bool
		wrong     bool
	}{
		{"every sample agrees and is right", []Operation{OperationJoin, OperationJoin, OperationJoin},
			OperationJoin, OperationJoin, true, false},
		{"every sample agrees and is wrong", []Operation{OperationReplace, OperationReplace, OperationReplace},
			OperationJoin, OperationReplace, true, true},
		{"the samples split", []Operation{OperationJoin, OperationReplace, OperationJoin},
			OperationJoin, OperationJoin, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := Resampled{Samples: tc.samples, Truth: tc.truth}
			answer, unanimous := r.Majority()
			assert.Equal(t, tc.majority, answer)
			assert.Equal(t, tc.unanimous, unanimous)
			assert.Equal(t, tc.unanimous, r.Unanimous())
			assert.Equal(t, tc.wrong, r.Wrong())
		})
	}
}

// The one bias that needs no number from the model: ask again, and let the
// samples disagreeing be the doubt.
func TestStability_DeclaringResolvesASplitToJoin(t *testing.T) {
	base := Declarations{Declarations: []Declaration{
		{Subject: "Alma", Attribute: "project", Value: "a", Year: 1,
			Truth: OperationReplace, Declared: OperationReplace},
		{Subject: "Alma", Attribute: "project", Value: "b", Year: 6,
			Truth: OperationJoin, Declared: OperationReplace},
	}}
	stable := Stability{Answers: []Resampled{
		// Settled, and settled on a replacement: taken as it stands.
		{Subject: "Alma", Attribute: "project", Value: "a", Year: 1, Truth: OperationReplace,
			Samples: []Operation{OperationReplace, OperationReplace, OperationReplace}},
		// Split: resolved to the failure that heals.
		{Subject: "Alma", Attribute: "project", Value: "b", Year: 6, Truth: OperationJoin,
			Samples: []Operation{OperationReplace, OperationJoin, OperationReplace}},
	}}
	declaring := stable.Declaring(base)

	settled := wrote(0, almaProject, "a", "first.")
	assert.Equal(t, OperationReplace, declaring(settled).Declared)

	split := wrote(5*daysPerYear+10, almaProject, "b", "second.")
	assert.Equal(t, OperationJoin, declaring(split).Declared)
}

// Whether the samples disagreeing says anything about the answer being
// wrong is the figure that decides whether any doubt-reading bias is
// possible at all, so it is computed rather than described.
func TestScoreStability_SeparatesSplitsOnWrongAnswersFromSplitsOnRight(t *testing.T) {
	scored := ScoreStability(Stability{Answers: []Resampled{
		{Truth: OperationReplace, Samples: []Operation{OperationReplace, OperationReplace, OperationReplace}},
		{Truth: OperationReplace, Samples: []Operation{OperationReplace, OperationJoin, OperationReplace}},
		{Truth: OperationJoin, Samples: []Operation{OperationReplace, OperationReplace, OperationReplace}},
		{Truth: OperationJoin, Samples: []Operation{OperationJoin, OperationReplace, OperationReplace}},
	}})

	assert.Equal(t, 4, scored.Asked)
	assert.Equal(t, 0.5, scored.Unanimous.Mean)
	assert.Equal(t, 1, scored.UnanimousWrong)
	assert.Equal(t, 2, scored.Split)
	assert.Equal(t, 0.5, scored.SplitAmongWrong.Mean)
	assert.Equal(t, 0.5, scored.SplitAmongRight.Mean)
	assert.Equal(t, 1, scored.Recovers, "one split sits on a write that should have joined")
	assert.Equal(t, 1, scored.Costs, "the other sits on a replacement that was right")
}
