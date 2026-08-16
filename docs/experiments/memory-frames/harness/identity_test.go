package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What a similarity-located write has to separate is a property of the
// workload, so it is read off the workload and not off any frame's score.
func TestMeasureIdentity_ReadsBothSimilaritiesOffTheWorkload(t *testing.T) {
	w, err := GenerateWorkload(7, Params{Years: 10, SessionsPerYear: 4, TurnsPerSession: 8, ThreadsPerClass: 2})
	require.NoError(t, err)

	out, err := MeasureIdentity(context.Background(), w, NewEmbedCounter())
	require.NoError(t, err)

	assert.Positive(t, out.Assertions, "the workload writes twice to some address")
	assert.Greater(t, out.Target.Mean, out.Distractor.Mean,
		"the value at the address is nearer than anything elsewhere")
	assert.LessOrEqual(t, out.Inverted, out.Assertions)
	assert.LessOrEqual(t, out.BelowGate, out.Assertions)
	assert.Equal(t, out.Assertions, out.Target.N)
	assert.Equal(t, out.Assertions, out.Distractor.N)
}

// A first assertion at an address has nothing to be separated from, so it is
// not counted. Counting it would put a similarity of zero in the target
// column and make the separation look wider than it is.
func TestMeasureIdentity_CountsOnlyTheWritesThatLandOnSomething(t *testing.T) {
	w, err := GenerateWorkload(7, Params{Years: 10, SessionsPerYear: 4, TurnsPerSession: 8, ThreadsPerClass: 2})
	require.NoError(t, err)

	out, err := MeasureIdentity(context.Background(), w, NewEmbedCounter())
	require.NoError(t, err)

	var written, addresses int
	for _, turn := range w.Turns {
		written += len(turn.Written)
	}
	seen := map[Address]bool{}
	for _, turn := range w.Turns {
		for _, one := range turn.Written {
			if !seen[one.Address] {
				seen[one.Address] = true
				addresses++
			}
		}
	}
	assert.Equal(t, written-addresses, out.Assertions)
}
