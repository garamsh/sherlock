package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A direction corrupts the writes that declare its operation and leaves the
// others alone, so the two failures never reach a frame mixed together.
func TestMisdeclaration_CorruptsOneDirectionAndLeavesTheOther(t *testing.T) {
	join := wrote(0, almaProject, "amber-quarry", "joins.")
	join.Declared = OperationJoin
	replace := wrote(1, almaProject, "copper-lantern", "replaces.")
	replace.Declared = OperationReplace

	tests := []struct {
		direction string
		join      Operation
		replace   Operation
	}{
		{directionJoinAsReplace, OperationReplace, OperationReplace},
		{directionReplaceAsJoin, OperationJoin, OperationJoin},
	}
	for _, tc := range tests {
		t.Run(tc.direction, func(t *testing.T) {
			// Rate 1 corrupts every eligible write, which is what makes the
			// direction's reach visible without depending on a draw.
			errs := Misdeclaration{Direction: tc.direction, Rate: 1, Seed: 7}
			assert.Equal(t, tc.join, errs.applyTo(join).Declared)
			assert.Equal(t, tc.replace, errs.applyTo(replace).Declared)
		})
	}
}

func TestMisdeclaration_CorruptsNothingAtRateZero(t *testing.T) {
	join := wrote(0, almaProject, "amber-quarry", "joins.")
	join.Declared = OperationJoin

	for _, direction := range directionNames {
		errs := Misdeclaration{Direction: direction, Rate: 0, Seed: 7}
		assert.Equal(t, join, errs.applyTo(join))
	}
}

// Which writes are corrupted comes from the turn's own index, so the same
// rate corrupts the same writes however the run reaches them.
func TestMisdeclaration_CorruptsTheSameWritesWhateverTheOrder(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)
	errs := Misdeclaration{Direction: directionReplaceAsJoin, Rate: 0.3, Seed: 7}

	first := errs.Flips(w)
	assert.NotEmpty(t, first)
	assert.Equal(t, first, errs.Flips(w))

	reversed := w
	reversed.Turns = make([]Turn, len(w.Turns))
	for i, turn := range w.Turns {
		reversed.Turns[len(w.Turns)-1-i] = turn
	}
	backwards := errs.Flips(reversed)
	assert.ElementsMatch(t, first, backwards)
}

func TestMisdeclaration_CorruptsAboutTheRateOfTheEligibleWrites(t *testing.T) {
	w, err := GenerateWorkload(7, Params{Years: 10, SessionsPerYear: 16, TurnsPerSession: 16, ThreadsPerClass: 20})
	require.NoError(t, err)

	for _, direction := range directionNames {
		t.Run(direction, func(t *testing.T) {
			eligible := eligibleWrites(w, direction)
			require.Positive(t, eligible)
			errs := Misdeclaration{Direction: direction, Rate: 0.25, Seed: 7}
			flips := len(errs.Flips(w))
			assert.InDelta(t, 0.25*float64(eligible), float64(flips), 0.15*float64(eligible))
		})
	}
}

// The healing asymmetry, which is the round's structural finding: a write
// that should have replaced and arrived as a join is repaired by the next
// correct replacement at that address, and a write that should have joined
// and arrived as a replacement is not repaired by anything the workload
// says afterwards, because nothing re-asserts a value the store destroyed.
func TestMeasureDamage_SeparatesWhatHealsFromWhatDoesNot(t *testing.T) {
	w, err := GenerateWorkload(7, Params{Years: 10, SessionsPerYear: 16, TurnsPerSession: 16, ThreadsPerClass: 20})
	require.NoError(t, err)

	damage := func(direction string) Damage {
		errs := Misdeclaration{Direction: direction, Rate: 1, Seed: 7}
		measured, err := Measure(context.Background(), w, frameAttributeDeclared, armOracle,
			NewEmbedCounter(), errs.applyTo)
		require.NoError(t, err)
		return MeasureDamage(errs.Flips(w), measured.Probes, w.Params.Years)
	}

	destroyed := damage(directionJoinAsReplace)
	assert.Positive(t, destroyed.Addresses)
	assert.Equal(t, 0.0, destroyed.Healed.Mean,
		"a value a wrong replacement destroyed is never asserted again, so nothing repairs it")

	accumulated := damage(directionReplaceAsJoin)
	assert.Positive(t, accumulated.Addresses)
	assert.Positive(t, accumulated.Healed.Mean,
		"a stale value a wrong join left is cleared by the next correct replacement")
	assert.Less(t, accumulated.Healed.Mean, 1.0,
		"an address with no later write carries the damage to the end")
}

// A wrong declaration costs nothing where the address holds nothing to
// destroy or to keep, which is why the classes each direction breaks are
// not the classes it touches.
func TestMeasureDamage_CountsNoDamageWhereTheAddressWasEmpty(t *testing.T) {
	ctx := context.Background()
	frame := NewAttributeFrame(arityDeclared)

	opening := wrote(0, almaProject, "amber-quarry", "Alma leads the amber-quarry project.")
	opening.Declared = OperationReplace
	errs := Misdeclaration{Direction: directionReplaceAsJoin, Rate: 1, Seed: 7}
	require.NoError(t, frame.Observe(ctx, errs.applyTo(opening)))

	injected, err := frame.Context(ctx, asked(400, almaProject))
	require.NoError(t, err)
	assert.True(t, Mentions(injected.Text, "amber-quarry"),
		"the first write to an address is the same either way it is declared")
}

func TestUniform_IsStableAndSpreadOverTheUnitInterval(t *testing.T) {
	assert.Equal(t, uniform(7, 42), uniform(7, 42))
	assert.NotEqual(t, uniform(7, 42), uniform(8, 42))
	assert.NotEqual(t, uniform(7, 42), uniform(7, 43))

	var sum float64
	const n = 4000
	for i := 0; i < n; i++ {
		v := uniform(7, i)
		require.GreaterOrEqual(t, v, 0.0)
		require.Less(t, v, 1.0)
		sum += v
	}
	assert.InDelta(t, 0.5, sum/n, 0.02)
}
