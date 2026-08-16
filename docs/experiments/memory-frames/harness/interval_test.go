package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A replacement closes the span it replaces rather than removing it, so the
// present is answered exactly and the past is still there.
func TestIntervalFrame_ClosesTheSpanARewriteEnds(t *testing.T) {
	ctx := context.Background()
	frame := NewIntervalFrame(aritySingle)

	require.NoError(t, frame.Observe(ctx, wrote(0, almaProject, "amber-quarry", "first.")))
	require.NoError(t, frame.Observe(ctx, wrote(400, almaProject, "copper-lantern", "second.")))

	injected, err := frame.Context(ctx, asked(800, almaProject))
	require.NoError(t, err)
	assert.True(t, Mentions(injected.Text, "copper-lantern"), "the open span answers the present")
	assert.False(t, Mentions(injected.Text, "amber-quarry"), "the closed span does not")

	size, err := frame.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, size.Units, "the closed span is still held")
}

// The question the workload never asks. Asking the frame as of a moment
// inside a closed span returns what was true then, which is the whole of
// what this structure does that an addressed store cannot.
func TestIntervalFrame_AnswersAsOfTheMomentItIsAsked(t *testing.T) {
	ctx := context.Background()
	frame := NewIntervalFrame(aritySingle)

	require.NoError(t, frame.Observe(ctx, wrote(0, almaProject, "amber-quarry", "first.")))
	require.NoError(t, frame.Observe(ctx, wrote(400, almaProject, "copper-lantern", "second.")))
	require.NoError(t, frame.Observe(ctx, wrote(800, almaProject, "ironwood-annex", "third.")))

	tests := []struct {
		name string
		day  int
		want string
	}{
		{"inside the first span", 200, "amber-quarry"},
		{"inside the second", 600, "copper-lantern"},
		{"inside the open one", 1000, "ironwood-annex"},
		{"on the day a span was closed", 400, "copper-lantern"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			injected, err := frame.Context(ctx, asked(tc.day, almaProject))
			require.NoError(t, err)
			for _, value := range []string{"amber-quarry", "copper-lantern", "ironwood-annex"} {
				assert.Equal(t, value == tc.want, Mentions(injected.Text, value), "%q in %q", value, injected)
			}
		})
	}
}

// A value asserted again after it was closed opens a second span. It was
// true, then it was not, and then it was again, and each of those is
// answerable at its own moment.
func TestIntervalFrame_OpensASecondSpanForAValueWrittenAgain(t *testing.T) {
	ctx := context.Background()
	frame := NewIntervalFrame(aritySingle)

	require.NoError(t, frame.Observe(ctx, wrote(0, almaProject, "amber-quarry", "first.")))
	require.NoError(t, frame.Observe(ctx, wrote(400, almaProject, "copper-lantern", "second.")))
	require.NoError(t, frame.Observe(ctx, wrote(800, almaProject, "amber-quarry", "back again.")))

	size, err := frame.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, size.Units, "two spans for the value that was true twice, one for the other")

	middle, err := frame.Context(ctx, asked(600, almaProject))
	require.NoError(t, err)
	assert.True(t, Mentions(middle.Text, "copper-lantern"))

	now, err := frame.Context(ctx, asked(1000, almaProject))
	require.NoError(t, err)
	assert.True(t, Mentions(now.Text, "amber-quarry"))
	assert.False(t, Mentions(now.Text, "copper-lantern"))
}

func TestIntervalFrame_HoldsNothingItCannotAddress(t *testing.T) {
	ctx := context.Background()
	frame := NewIntervalFrame(aritySingle)

	require.NoError(t, frame.Observe(ctx, Turn{Index: 0, Day: 0, At: start,
		Text: "Alma leads the amber-quarry project."}))

	size, err := frame.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, Size{}, size)

	injected, err := frame.Context(ctx, Turn{Index: 1, Day: 0, At: start, Text: "Which project does Alma lead?"})
	require.NoError(t, err)
	assert.Empty(t, injected.Text)
}
