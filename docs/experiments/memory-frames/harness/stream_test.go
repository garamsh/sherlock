package main

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// observed returns a turn carrying text and no assertion, which is what the
// stream designs read.
func observedAt(day int, text string) Turn {
	return Turn{Index: day, Day: day, At: start.AddDate(0, 0, day), Text: text}
}

func TestStreamFrame_ReturnsTheRecordMostRelevantToTheTurn(t *testing.T) {
	ctx := context.Background()
	frame := NewStreamFrame(ruleGenerativeAgents, NewEmbedCounter(), 0)

	require.NoError(t, frame.Observe(ctx, observedAt(0, "Alma leads the amber-quarry project.")))
	for i := 1; i < 60; i++ {
		require.NoError(t, frame.Observe(ctx, observedAt(i, fmt.Sprintf("Otto asked about the kettle %d times.", i))))
	}

	injected, err := frame.Context(ctx, observedAt(100, "Which project does Alma lead?"))
	require.NoError(t, err)
	assert.True(t, Mentions(injected.Text, "amber-quarry"))
	assert.Equal(t, 60, injected.Scanned, "the whole stream is ranked to answer one question")
}

// Nothing ever leaves the stream in Generative Agents, and the record's
// reading of what that costs rests on it.
func TestStreamFrame_KeepsEveryObservationUnderGenerativeAgents(t *testing.T) {
	ctx := context.Background()
	frame := NewStreamFrame(ruleGenerativeAgents, NewEmbedCounter(), 0)

	for i := 0; i < 200; i++ {
		require.NoError(t, frame.Observe(ctx, observedAt(i*10, fmt.Sprintf("turn number %d.", i))))
	}

	size, err := frame.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, 200, size.Units)
	assert.Greater(t, size.StoredBytes, size.ContentBytes, "a record costs the vector it is retrieved by")
}

// MemoryBank's forgetting curve is what separates it from the stream it is
// otherwise identical to: a record goes once the days since it was last
// recalled pass the strength its rehearsals have built, and a retrieval is
// a rehearsal.
func TestStreamFrame_ForgetsWhatTheCurveTakesAndKeepsWhatWasRehearsed(t *testing.T) {
	ctx := context.Background()
	// A budget that holds one record, so the read rehearses the record it
	// answers with and not everything the frame is holding.
	frame := NewStreamFrame(ruleMemoryBank, NewEmbedCounter(), 10)

	require.NoError(t, frame.Observe(ctx, observedAt(0, "Alma leads the amber-quarry project.")))
	require.NoError(t, frame.Observe(ctx, observedAt(0, "Otto keeps the copper-lantern rota.")))
	for day := 1; day < 8; day++ {
		injected, err := frame.Context(ctx, observedAt(day, "Which project does Alma lead?"))
		require.NoError(t, err)
		require.True(t, Mentions(injected.Text, "amber-quarry"), "the rehearsed record is returned on day %d", day)
		require.NoError(t, frame.Observe(ctx, observedAt(day, fmt.Sprintf("nothing happened on day %d.", day))))
	}

	held, err := frame.Held(ctx)
	require.NoError(t, err)
	var kept, dropped bool
	for _, text := range held {
		kept = kept || Mentions(text, "amber-quarry")
		dropped = dropped || Mentions(text, "copper-lantern")
	}
	assert.True(t, kept, "a record a read keeps returning is rehearsed and stays")
	assert.False(t, dropped, "a record no read returns falls under the curve")
}

// A reflection is a record like any other, and two things stop it from
// eating the structure: it is bounded by what a context holds, and no later
// reflection reads it.
func TestStreamFrame_BoundsAReflectionAndNeverReadsOneIntoAnother(t *testing.T) {
	ctx := context.Background()
	frame := NewStreamFrame(ruleReflection, NewEmbedCounter(), 0)

	for i := 0; i < 400; i++ {
		turn := observedAt(i, fmt.Sprintf("Alma leads the amber-%d project.", i))
		turn.Written = []Written{{Fact: Fact{Address: almaProject, Value: fmt.Sprintf("amber-%d", i)}}}
		require.NoError(t, frame.Observe(ctx, turn))
	}

	held, err := frame.Held(ctx)
	require.NoError(t, err)
	// Every observation carries a fact, so the summed importance reaches the
	// trigger every reflectionTrigger/semanticImportance of them, and each
	// reflection is a record that adds nothing to the next trigger.
	reflections := int(math.Floor(400 * semanticImportance / reflectionTrigger))
	assert.Equal(t, 400+reflections, len(held))
	for _, text := range held {
		assert.LessOrEqual(t, len(text), documentLimit, "no record outgrows what a context holds")
	}
}

func TestStreamFrame_FillsTheContextToTheBudgetAndNoFurther(t *testing.T) {
	ctx := context.Background()
	frame := NewStreamFrame(ruleGenerativeAgents, NewEmbedCounter(), 0)

	for i := 0; i < 400; i++ {
		require.NoError(t, frame.Observe(ctx, observedAt(i, fmt.Sprintf("Alma leads the amber-quarry project, again for the %dth time.", i))))
	}

	injected, err := frame.Context(ctx, observedAt(500, "Which project does Alma lead?"))
	require.NoError(t, err)
	assert.LessOrEqual(t, len(injected.Text), documentLimit,
		"the stream fills the same context the document frame fills")
	assert.Positive(t, len(injected.Text))
}

func TestNormalize_ScalesBySpanAndFlattensWhereThereIsNone(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
		want   []float64
	}{
		{"a spread scales to the ends", []float64{1, 2, 3}, []float64{0, 0.5, 1}},
		{"no spread says nothing", []float64{2, 2, 2}, []float64{0, 0, 0}},
		{"one value says nothing", []float64{7}, []float64{0}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			normalize(tc.values)
			assert.Equal(t, tc.want, tc.values)
		})
	}
}

// The retune (#489): the same curve with its unit rescaled from the
// paper's day to the workload's probe interval forgets nothing a year
// apart that the day-unit curve takes inside a week — the difference
// between the two rows is the constant, not the mechanism.
func TestStreamFrame_RescaledCurveKeepsWhatTheDayCurveForgets(t *testing.T) {
	ctx := context.Background()
	build := func() Frame {
		frame := newStreamFrameAtUnit(ruleMemoryBank, NewEmbedCounter(), 0, 24*365.25)
		require.NoError(t, frame.Observe(ctx, observedAt(0, "Alma leads the amber-quarry project.")))
		return frame
	}
	dayCurve := NewStreamFrame(ruleMemoryBank, NewEmbedCounter(), 0)
	require.NoError(t, dayCurve.Observe(ctx, observedAt(0, "Alma leads the amber-quarry project.")))

	aYearLater := observedAt(365, "a year passes.")
	yearCurve := build()
	require.NoError(t, yearCurve.Observe(ctx, aYearLater))
	require.NoError(t, dayCurve.Observe(ctx, aYearLater))

	heldYear, err := yearCurve.Held(ctx)
	require.NoError(t, err)
	heldDay, err := dayCurve.Held(ctx)
	require.NoError(t, err)

	var yearKept, dayKept bool
	for _, text := range heldYear {
		yearKept = yearKept || Mentions(text, "amber-quarry")
	}
	for _, text := range heldDay {
		dayKept = dayKept || Mentions(text, "amber-quarry")
	}
	assert.True(t, yearKept, "at the probe interval's scale a record survives to the next checkpoint")
	assert.False(t, dayKept, "at the paper's day scale the same record is gone inside it")
}
