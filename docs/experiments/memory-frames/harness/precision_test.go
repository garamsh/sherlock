package main

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewFigure_CarriesTheIntervalItsSampleSupports(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
		mean   float64
		half   float64
		n      int
	}{
		{"no sample is no figure", nil, 0, 0, 0},
		{"one value has a mean and no spread to measure", []float64{0.5}, 0.5, 0, 1},
		{"identical values have no width", []float64{1, 1, 1, 1}, 1, 0, 4},
		// The sample standard deviation of two zeros and two ones is the
		// square root of a third, and the half-width is Z times it over the
		// square root of the sample.
		{"a half-and-half sample", []float64{0, 1, 0, 1}, 0.5, Z * math.Sqrt(1.0/3.0) / 2, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := NewFigure(tc.values)
			assert.InDelta(t, tc.mean, f.Mean, 1e-9)
			assert.InDelta(t, tc.half, f.Half, 1e-9)
			assert.Equal(t, tc.n, f.N)
		})
	}
}

func TestHalfWidthAt_NarrowsWithTheSquareRootOfTheSample(t *testing.T) {
	f := NewFigure([]float64{0, 1, 0, 1})

	assert.InDelta(t, f.Half, HalfWidthAt(f, f.N), 1e-9)
	assert.InDelta(t, f.Half/2, HalfWidthAt(f, 4*f.N), 1e-9)
	assert.Zero(t, HalfWidthAt(Figure{}, 100))
	assert.Zero(t, HalfWidthAt(f, 0))
}
