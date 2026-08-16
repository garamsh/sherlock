package main

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFigure_ReportsTheMeanWithItsHalfWidth(t *testing.T) {
	f := NewFigure([]float64{0, 1, 0, 1})
	assert.InDelta(t, 0.5, f.Mean, 1e-9)
	assert.Equal(t, 4, f.N)
	// sd of {0,1,0,1} is 0.577350, and 1.96 * sd / sqrt(4) is 0.565757.
	assert.InDelta(t, Z*math.Sqrt(1.0/3.0)/2, f.Half, 1e-9)

	constant := NewFigure([]float64{0.25, 0.25, 0.25})
	assert.InDelta(t, 0.25, constant.Mean, 1e-9)
	assert.Zero(t, constant.Half)

	assert.Equal(t, Figure{}, NewFigure(nil))
}

func TestHalfWidthAt_NarrowsWithTheSquareRootOfTheProbeCount(t *testing.T) {
	f := NewFigure([]float64{0, 1, 0, 1})
	assert.InDelta(t, f.Half/2, HalfWidthAt(f, 4*f.N), 1e-9)
	assert.InDelta(t, f.Half, HalfWidthAt(f, f.N), 1e-9)
	assert.Zero(t, HalfWidthAt(Figure{}, 100))
}

func TestSummarize_DividesHitsByThePlantedAnswersAndByTheReturnedCount(t *testing.T) {
	result := SizeResult{Probes: []ProbeResult{
		{Relevant: 4, HitsAt1: 1, HitsAt5: 2, HitsAtLimit: 3, ReturnedAtLimit: 10,
			HitsAtBudget: 2, ReturnedAtBudget: 4, BestRank: 1},
		{Relevant: 2, HitsAt1: 0, HitsAt5: 1, HitsAtLimit: 1, ReturnedAtLimit: 10,
			HitsAtBudget: 0, ReturnedAtBudget: 5, BestRank: 7},
	}}
	f := Summarize(result)

	require.Equal(t, 2, f.RecallAtLimit.N)
	assert.InDelta(t, (3.0/4+1.0/2)/2, f.RecallAtLimit.Mean, 1e-9)
	assert.InDelta(t, (3.0/10+1.0/10)/2, f.PrecisionAtLimit.Mean, 1e-9)
	assert.InDelta(t, (1.0/4+0.0/2)/2, f.RecallAt1.Mean, 1e-9)
	assert.InDelta(t, (1.0+0.0)/2, f.PrecisionAt1.Mean, 1e-9)
	assert.InDelta(t, (2.0/5+1.0/5)/2, f.PrecisionAt5.Mean, 1e-9)
	assert.InDelta(t, (2.0/4+0.0/5)/2, f.PrecisionAtBudget.Mean, 1e-9)
	assert.InDelta(t, 4.0, f.BestRank.Mean, 1e-9)
}
