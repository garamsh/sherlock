package main

import "math"

// Z is the standard normal quantile for a 95% interval.
const Z = 1.96

// Figure is one published number with the interval its sample supports.
// Half is the 95% half-width from the sample standard deviation of the
// per-probe values, so a figure and its uncertainty are never separated. It
// follows the shape memory-retrieval/'s harness established, which is the
// reason it is a copy: that harness was a separate command, and a record's
// figures are only reproducible while the code that computed them is
// committed beside them. #582 removed that harness with the store it
// measured, and this definition was already independent of it.
//
// One approximation remains and it widens rather than narrows the interval:
// a year's probes are answered from one store, so their errors are if
// anything positively correlated.
type Figure struct {
	Mean float64 `json:"mean"`
	Half float64 `json:"half_width"`
	N    int     `json:"n"`
}

// NewFigure returns the mean of the values with the 95% half-width their
// count supports.
func NewFigure(values []float64) Figure {
	n := len(values)
	if n == 0 {
		return Figure{}
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(n)
	if n == 1 {
		return Figure{Mean: mean, N: n}
	}
	var ss float64
	for _, v := range values {
		ss += (v - mean) * (v - mean)
	}
	sd := math.Sqrt(ss / float64(n-1))
	return Figure{Mean: mean, Half: Z * sd / math.Sqrt(float64(n)), N: n}
}

// HalfWidthAt reports what the half-width of a figure would be at a
// different sample count, holding its observed spread fixed. It answers
// whether a wider workload would separate two figures this one cannot.
func HalfWidthAt(f Figure, samples int) float64 {
	if f.N == 0 || samples <= 0 {
		return 0
	}
	return f.Half * math.Sqrt(float64(f.N)) / math.Sqrt(float64(samples))
}
