package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// holds reports whether the frame still holds the value anywhere.
func holds(t *testing.T, frame Frame, value string) bool {
	t.Helper()
	held, err := frame.Held(context.Background())
	require.NoError(t, err)
	for _, text := range held {
		if Mentions(text, value) {
			return true
		}
	}
	return false
}

// The round's structural claim, constructed rather than argued: a fact is
// written, never once read back, and then corrected. A store that has to
// find what it supersedes cannot correct what it cannot retrieve; a store
// that computes the address writes there whether anything ever read it.
//
// The distractors are what makes this the question rather than a formality.
// A correction arriving at a store holding one row finds that row whatever
// the ranking does; the case worth measuring is the one a decade produces,
// where the row it must find is among many that look like it.
func TestCoupling_AWriteReachesWhatNoReadReturned(t *testing.T) {
	ctx := context.Background()
	frozen := Address{Subject: "Alma", Attribute: "project"}

	tests := []struct {
		name  string
		build func() (Frame, error)
		// declares is whether the frame is told what the write does.
		declares bool
	}{
		{"a store that finds what it supersedes", func() (Frame, error) {
			return NewShippedFrame(t.TempDir(), NewEmbedCounter())
		}, false},
		{"a store that computes the address", func() (Frame, error) {
			return NewAttributeFrame(arityDeclared), nil
		}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			frame, err := tc.build()
			require.NoError(t, err)
			defer frame.Close()

			write := func(day int, address Address, value, text string, op Operation) {
				turn := wrote(day, address, value, text)
				if tc.declares {
					turn.Declared = op
				}
				require.NoError(t, frame.Observe(ctx, turn))
			}

			write(0, frozen, "amber-quarry", "Alma leads the amber-quarry project.", OperationReplace)

			// A decade of other people's facts, in the same words. Nothing
			// is ever asked, so nothing is ever read back.
			for i, subject := range subjects {
				if subject == frozen.Subject {
					continue
				}
				address := Address{Subject: subject, Attribute: "project"}
				value := fmt.Sprintf("filler-%d", i)
				write(i+1, address, value,
					fmt.Sprintf("%s leads the %s project.", subject, value), OperationReplace)
			}
			require.True(t, holds(t, frame, "amber-quarry"), "the fact is there before the correction")

			write(3000, frozen, "copper-lantern",
				"Alma leads the copper-lantern project from now on.", OperationReplace)

			corrected := !holds(t, frame, "amber-quarry")
			injected, err := frame.Context(ctx, asked(3100, frozen))
			require.NoError(t, err)
			t.Logf("%s: correction applied to a never-read fact: %v", tc.name, corrected)

			if tc.declares {
				assert.True(t, corrected,
					"an address is computed from the sentence, so the write lands whether or not a read ever returned it")
				assert.True(t, Mentions(injected.Text, "copper-lantern"))
				assert.False(t, Mentions(injected.Text, "amber-quarry"))
				return
			}
			assert.False(t, corrected,
				"a store that supersedes what its own lookup surfaces leaves untouched what the lookup does not reach")
		})
	}
}

// The control, and it does not say what it was written to say — which is
// worth more than if it had.
//
// With the row it must correct sitting alone, the same store still does not
// correct it. Its supersession fires on the similarity between the two
// sentences, and a correction phrased as a correction shares few enough
// words with what it corrects to fall under the threshold — which is
// `memory-retrieval/`'s finding arriving here. So for this store the
// workload cannot separate the two causes: a correction that misses because
// the row was not retrieved looks the same as one that misses because the
// row was retrieved and judged unrelated. The subtest above is the coupling
// claim; this one bounds what may be read off it.
func TestCoupling_TheSameStoreFailsToCorrectEvenWhatItCanFind(t *testing.T) {
	ctx := context.Background()
	address := Address{Subject: "Alma", Attribute: "project"}

	tests := []struct {
		name       string
		correction string
		corrected  bool
	}{
		{"a correction phrased as one", "Alma leads the copper-lantern project from now on.", false},
		{"a restatement in the same words", "Alma leads the copper-lantern project.", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			frame, err := NewShippedFrame(t.TempDir(), NewEmbedCounter())
			require.NoError(t, err)
			defer frame.Close()

			require.NoError(t, frame.Observe(ctx, wrote(0, address, "amber-quarry",
				"Alma leads the amber-quarry project.")))
			require.NoError(t, frame.Observe(ctx, wrote(3000, address, "copper-lantern", tc.correction)))

			assert.Equal(t, tc.corrected, !holds(t, frame, "amber-quarry"),
				"whether the correction lands is decided by how much the two sentences share")
		})
	}
}

// A correction is scored on what the structure holds and not on what it
// returned, because a value no read surfaces is missing from a context
// whether or not anything corrected it.
func TestMeasureCoupling_ScoresACorrectionOnWhatIsStillHeld(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)
	ended := corrections(w)
	require.NotEmpty(t, ended)

	// Every value still held at its correction year counts as uncorrected,
	// whichever side of the split it falls on.
	held := map[int]map[string]bool{}
	firstReached := map[string]int{}
	for _, c := range ended {
		if held[c.year] == nil {
			held[c.year] = map[string]bool{}
		}
		held[c.year][c.value] = true
	}
	none := MeasureCoupling(w, held, firstReached)
	assert.Zero(t, none.Reached.Applied)
	assert.Zero(t, none.Unreached.Applied)
	assert.Equal(t, len(ended), none.Unreached.Corrections,
		"with nothing ever reached, every correction is on the unreached side")

	// Reading a value before its correction moves it to the other side.
	for _, c := range ended {
		firstReached[c.value] = c.year - 1
	}
	all := MeasureCoupling(w, held, firstReached)
	assert.Equal(t, len(ended), all.Reached.Corrections)
	assert.Zero(t, all.Unreached.Corrections)
}

// A thread that returns to a value it left does not correct that value: the
// workload's reverted class ends the replacement, not the original.
func TestCorrections_DoNotCountAValueAThreadReturnsTo(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)

	byThread := map[string][]correction{}
	for _, c := range corrections(w) {
		byThread[c.subject+"|"+c.attribute] = append(byThread[c.subject+"|"+c.attribute], c)
	}
	for _, thread := range w.Threads {
		ended := byThread[thread.Subject+"|"+thread.Attribute]
		_, stale := thread.TruthAt(w.Params.Years)
		switch thread.Class {
		case classCarried, classFresh, classParallel, classRefined:
			assert.Empty(t, ended, "%s ends no value", thread.Class)
		case classReverted:
			require.Len(t, ended, 2, "a reverted thread ends its first value and then the replacement")
			assert.Equal(t, []string{ended[1].value}, stale,
				"only the replacement is still ended at the last year; the first value came back")
		case classReplaced:
			require.Len(t, ended, 2)
			assert.ElementsMatch(t, []string{ended[0].value, ended[1].value}, stale)
		}
	}
}
