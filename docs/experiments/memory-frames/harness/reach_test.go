package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What counts as a value in a context has to be what counts as a value in a
// probe, or the frozen share and the recall figure would be reading the
// same text by two different rules.
func TestValuesIn_FindsAValueWhereTheCreditRuleDoes(t *testing.T) {
	planted := map[string]bool{"amber-quarry": true, "copper-lantern": true}
	tests := []struct {
		text string
		want []string
	}{
		{"Alma leads the amber-quarry project.", []string{"amber-quarry"}},
		{"Alma amber-quarry and copper-lantern.", []string{"amber-quarry", "copper-lantern"}},
		{"Alma leads amber-quarryside.", nil},
		{"Alma leads the amber project.", nil},
		{"AMBER-QUARRY", []string{"amber-quarry"}},
		{"amber-quarry amber-quarry", []string{"amber-quarry"}},
		{"", nil},
	}
	for _, tc := range tests {
		t.Run(tc.text, func(t *testing.T) {
			found := valuesIn(tc.text, planted)
			assert.Equal(t, tc.want, found)
			for value := range planted {
				assert.Equal(t, Mentions(tc.text, value), contains(found, value),
					"the two rules disagree about %q in %q", value, tc.text)
			}
		})
	}
}

// Forgetting what no read reached costs recall only where a later
// checkpoint still needs it, so what is needed later is read off the
// workload rather than off any structure.
func TestNeededAfter_NarrowsAsTheSpanRunsOut(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)

	assert.NotEmpty(t, neededAfter(w, 1))
	assert.Empty(t, neededAfter(w, w.Params.Years), "nothing is needed after the last checkpoint")
	assert.GreaterOrEqual(t, len(neededAfter(w, 1)), len(neededAfter(w, w.Params.Years-1)))

	// A carried fact is required at the last year, so it is needed after
	// every year before it — which is why a rule keyed on re-assertion
	// would eat it and one keyed on reaching it does not.
	for _, thread := range w.Threads {
		if thread.Class != classCarried {
			continue
		}
		assert.Contains(t, neededAfter(w, 1), thread.Asserts[0].Value)
	}
}

// A frame that answers from what it holds reaches everything it holds, and
// a frame that answers nothing reaches none of it. The frozen count is the
// difference, and it is read the same way for either.
func TestMeasure_CountsNothingFrozenWhereEveryHeldValueIsReached(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)

	answering := measureWith(t, &transcriptFrame{}, w, armOracle)
	last := answering.Reach[len(answering.Reach)-1]
	assert.Positive(t, last.Held)
	assert.Equal(t, 0, last.Frozen, "a frame returning everything it holds freezes nothing")
	assert.Equal(t, last.Held, last.Reached)

	silent := measureWith(t, silentFrame{}, w, armOracle)
	quiet := silent.Reach[len(silent.Reach)-1]
	assert.Equal(t, 0, quiet.Held, "a frame holding nothing has nothing to freeze")
	assert.Equal(t, 0.0, quiet.SubjectReach.Mean)
}
