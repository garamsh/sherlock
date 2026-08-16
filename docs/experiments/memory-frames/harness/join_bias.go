package main

import (
	"context"
	"fmt"
)

// biasShares are the points the trade is measured at, from taking every
// declaration as it comes to joining whatever the declaration says.
var biasShares = []float64{0, 0.25, 0.50, 0.75, 1}

// JoinBias resolves a share of the writes that declare a replacement to a
// join instead. It is the only way to express "err toward the cheaper
// failure" without asking a model how sure it is: a share is a policy, not
// a mechanism, and it needs nothing from the write except what it already
// says.
//
// It is a measuring instrument and not a design. The two ends are
// structures anyone could build — take the declaration, or never destroy —
// and every point between them is a share of writes chosen by nothing,
// which no structure implements. What the interior is for is the shape of
// the trade between the two ends, which is what a chooser needs and what
// the record did not have.
type JoinBias struct {
	Share float64 `json:"share"`
	Seed  int64   `json:"seed"`
}

// over returns the declarations the inner source produces, with a share of
// the replacements resolved to joins. The draw comes from the turn's index,
// so a share resolves the same writes whatever order a run reaches them in.
func (b JoinBias) over(inner Declaring) Declaring {
	return func(t Turn) Turn {
		if inner != nil {
			t = inner(t)
		}
		if len(t.Written) == 0 || b.Share <= 0 {
			return t
		}
		written := make([]Written, len(t.Written))
		copy(written, t.Written)
		for i := range written {
			if written[i].Declared != OperationReplace {
				continue
			}
			if uniform(b.Seed, drawKey(t.Index, i)) < b.Share {
				written[i].Declared = OperationJoin
			}
		}
		t.Written = written
		return t
	}
}

// BiasResult is the frame at one point on that range, with what the
// destroying errors underneath it did.
type BiasResult struct {
	Share float64 `json:"share"`
	// Recovered counts the model's destroying errors this share undid: a
	// write that should have joined, arrived as a replacement, and was
	// resolved back to a join by the bias rather than by knowing anything.
	Recovered int          `json:"destroying_errors_recovered"`
	Destroyed int          `json:"destroying_errors_left"`
	Years     []YearResult `json:"years"`
	Sizes     []SizeAt     `json:"sizes"`
}

// MeasureJoinBias runs the declared-operation frame on the model's own
// answers at every share, so the curve is drawn through the error profile a
// model actually has rather than through an average one.
func MeasureJoinBias(ctx context.Context, w Workload, d Declarations) ([]BiasResult, error) {
	var out []BiasResult
	for _, share := range biasShares {
		bias := JoinBias{Share: share, Seed: w.Seed}
		declaring := bias.over(d.Declaring())
		measured, err := Measure(ctx, w, frameAttributeDeclared, armOracle, NewEmbedCounter(), declaring)
		if err != nil {
			return nil, fmt.Errorf("join bias %v: %w", share, err)
		}
		result := BiasResult{Share: share, Years: measured.Years, Sizes: measured.Sizes}
		result.Recovered, result.Destroyed = countRecovered(w, d, declaring)
		out = append(out, result)
	}
	return out, nil
}

// countRecovered splits the model's destroying errors into the ones the
// bias turned back into joins and the ones it left destroying.
func countRecovered(w Workload, d Declarations, declaring Declaring) (recovered, left int) {
	wrong := map[string]bool{}
	for _, one := range d.Declarations {
		if one.Truth == OperationJoin && !one.Correct() {
			wrong[one.key()] = true
		}
	}
	for _, t := range w.Turns {
		biased := declaring(t)
		for i, written := range t.Written {
			asserted := Declaration{
				Subject: written.Subject, Attribute: written.Attribute,
				Value: written.Value, Year: t.Day/daysPerYear + 1,
			}
			if !wrong[asserted.key()] {
				continue
			}
			if biased.Written[i].Declared == OperationJoin {
				recovered++
				continue
			}
			left++
		}
	}
	return recovered, left
}
