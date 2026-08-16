package main

import "math"

// Z is the standard normal quantile for a 95% interval.
const Z = 1.96

// Figure is one published number with the interval its probe count
// supports. Half is the 95% half-width from the sample standard deviation
// of the per-probe values, so a figure and its uncertainty are never
// separated. Round 4 of the memory-creation experiment could only
// approximate this from family means (harness/round4/precision.py); here
// every probe's own value is committed, so the interval is computed from
// them directly.
//
// One approximation remains, and it widens rather than narrows the
// interval: the probes share one store and one query path, so their
// errors are if anything positively correlated.
type Figure struct {
	Mean float64 `json:"mean"`
	Half float64 `json:"half_width"`
	N    int     `json:"n"`
}

// Figures are the metrics reported at one store size. AtLimit is the
// shipped default search limit and AtBudget is the shipped recall token
// budget; both are recorded in the run header.
type Figures struct {
	RecallAt1         Figure `json:"recall_at_1"`
	PrecisionAt1      Figure `json:"precision_at_1"`
	RecallAt5         Figure `json:"recall_at_5"`
	PrecisionAt5      Figure `json:"precision_at_5"`
	RecallAtLimit     Figure `json:"recall_at_limit"`
	PrecisionAtLimit  Figure `json:"precision_at_limit"`
	RecallAtBudget    Figure `json:"recall_at_budget"`
	PrecisionAtBudget Figure `json:"precision_at_budget"`
	ReturnedAtBudget  Figure `json:"returned_at_budget"`
	// BestRank is where the highest-ranked planted answer sits in the
	// ranked store, averaged over the probes. It reads against the store
	// size in the same row: 1 is a perfect ranking, size/2 is chance.
	BestRank Figure `json:"best_rank"`
	// ReturnedAge and ReturnedSalience describe what the read path
	// returned, against the store means in the same row.
	ReturnedAge      Figure `json:"returned_age_hours"`
	ReturnedSalience Figure `json:"returned_salience"`
}

// Summarize computes every figure from the per-probe results.
func Summarize(r SizeResult) Figures {
	var (
		recall1, precision1      []float64
		recall5, precision5      []float64
		recallLim, precisionLim  []float64
		recallBud, precisionBud  []float64
		returnedBud, bestRanks   []float64
		returnedAge, returnedSal []float64
	)
	for _, p := range r.Probes {
		recall1 = append(recall1, ratio(p.HitsAt1, p.Relevant))
		precision1 = append(precision1, ratio(p.HitsAt1, 1))
		recall5 = append(recall5, ratio(p.HitsAt5, p.Relevant))
		precision5 = append(precision5, ratio(p.HitsAt5, 5))
		recallLim = append(recallLim, ratio(p.HitsAtLimit, p.Relevant))
		precisionLim = append(precisionLim, ratio(p.HitsAtLimit, p.ReturnedAtLimit))
		recallBud = append(recallBud, ratio(p.HitsAtBudget, p.Relevant))
		precisionBud = append(precisionBud, ratio(p.HitsAtBudget, p.ReturnedAtBudget))
		returnedBud = append(returnedBud, float64(p.ReturnedAtBudget))
		bestRanks = append(bestRanks, float64(p.BestRank))
		returnedAge = append(returnedAge, p.ReturnedAgeHours)
		returnedSal = append(returnedSal, p.ReturnedSalience)
	}
	return Figures{
		RecallAt1:         NewFigure(recall1),
		PrecisionAt1:      NewFigure(precision1),
		RecallAt5:         NewFigure(recall5),
		PrecisionAt5:      NewFigure(precision5),
		RecallAtLimit:     NewFigure(recallLim),
		PrecisionAtLimit:  NewFigure(precisionLim),
		RecallAtBudget:    NewFigure(recallBud),
		PrecisionAtBudget: NewFigure(precisionBud),
		ReturnedAtBudget:  NewFigure(returnedBud),
		BestRank:          NewFigure(bestRanks),
		ReturnedAge:       NewFigure(returnedAge),
		ReturnedSalience:  NewFigure(returnedSal),
	}
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
// different probe count, holding its observed spread fixed. It answers
// whether a wider probe set would separate two figures this one cannot.
func HalfWidthAt(f Figure, probes int) float64 {
	if f.N == 0 || probes <= 0 {
		return 0
	}
	return f.Half * math.Sqrt(float64(f.N)) / math.Sqrt(float64(probes))
}

func ratio(hits, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}
