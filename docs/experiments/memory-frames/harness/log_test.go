package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogFrame_KeepsEveryObservationAndProjectsTheNewest(t *testing.T) {
	ctx := context.Background()
	frame := NewLogFrame()

	require.NoError(t, frame.Observe(ctx, wrote(0, almaProject, "amber-quarry", "first.")))
	require.NoError(t, frame.Observe(ctx, wrote(400, almaProject, "copper-lantern", "second.")))

	injected, err := frame.Context(ctx, asked(800, almaProject))
	require.NoError(t, err)
	assert.True(t, Mentions(injected.Text, "copper-lantern"))
	assert.False(t, Mentions(injected.Text, "amber-quarry"))

	size, err := frame.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, size.Units, "both events are kept whatever the projection returns")
}

// The structure's one distinguishing property: no event is lost, so the
// arity decision can be taken again. The same log read the other way is a
// different memory with nothing re-observed.
func TestLogFrame_YieldsADifferentMemoryUnderADifferentProjection(t *testing.T) {
	ctx := context.Background()
	events := []event{
		{address: almaProject, value: "amber-quarry", at: start},
		{address: almaProject, value: "copper-lantern", at: start.AddDate(0, 0, 400)},
	}

	newest := &logFrame{events: events, project: projectNewest}
	every := &logFrame{events: events, project: projectEvery}

	fromNewest, err := newest.Context(ctx, asked(800, almaProject))
	require.NoError(t, err)
	fromEvery, err := every.Context(ctx, asked(800, almaProject))
	require.NoError(t, err)

	assert.False(t, Mentions(fromNewest.Text, "amber-quarry"))
	assert.True(t, Mentions(fromEvery.Text, "amber-quarry"),
		"the value the first projection dropped is still in the log")
	assert.True(t, Mentions(fromEvery.Text, "copper-lantern"))
}

// A value asserted twice is two events, because the log holds what was said
// rather than what is true.
func TestLogFrame_AppendsAnEventForEveryAssertion(t *testing.T) {
	ctx := context.Background()
	frame := NewLogFrame()

	require.NoError(t, frame.Observe(ctx, wrote(0, almaProject, "amber-quarry", "first.")))
	require.NoError(t, frame.Observe(ctx, wrote(400, almaProject, "copper-lantern", "second.")))
	require.NoError(t, frame.Observe(ctx, wrote(800, almaProject, "amber-quarry", "back again.")))

	size, err := frame.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, size.Units)

	injected, err := frame.Context(ctx, asked(1000, almaProject))
	require.NoError(t, err)
	assert.True(t, Mentions(injected.Text, "amber-quarry"), "the newest event wins whichever value it carries")
	assert.False(t, Mentions(injected.Text, "copper-lantern"))
}

func TestLogFrame_AnswersOnlyTheAddressAsked(t *testing.T) {
	ctx := context.Background()
	frame := NewLogFrame()
	other := Address{Subject: "Otto", Attribute: "clinic"}

	require.NoError(t, frame.Observe(ctx, wrote(0, almaProject, "amber-quarry", "first.")))
	require.NoError(t, frame.Observe(ctx, wrote(1, other, "copper-lantern", "otto's.")))

	injected, err := frame.Context(ctx, asked(2, almaProject))
	require.NoError(t, err)
	assert.True(t, Mentions(injected.Text, "amber-quarry"))
	assert.False(t, Mentions(injected.Text, "copper-lantern"))
}

func TestLogFrame_HoldsNothingItCannotAddress(t *testing.T) {
	ctx := context.Background()
	frame := NewLogFrame()

	require.NoError(t, frame.Observe(ctx, Turn{Index: 0, Day: 0, At: start,
		Text: "Alma leads the amber-quarry project."}))

	size, err := frame.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, Size{}, size)
}
