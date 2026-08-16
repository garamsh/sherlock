package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
)

// The two directions a declaration can be wrong in. They are never blended
// into one rate, here or in any table: a write that should have joined and
// arrives as a replacement ends a value that was still true, and the
// information is gone; a write that should have replaced and arrives as a
// join leaves a value that has stopped being true, and the information is
// wrong but still there. Over a decade those compound differently, and one
// number covering both would hide which failure a structure has.
const (
	// directionJoinAsReplace destroys: a value that is still true is ended
	// by a write that was only ever adding another.
	directionJoinAsReplace = "join-as-replace"
	// directionReplaceAsJoin accumulates: a value that has stopped being
	// true stays beside the one that replaced it.
	directionReplaceAsJoin = "replace-as-join"
)

// directionNames is the order every table reports the directions in.
var directionNames = []string{directionJoinAsReplace, directionReplaceAsJoin}

// Declaring is where a run's declarations come from. The workload's own
// declaration is the identity, a rate corrupts it in one direction, and a
// model replaces it with what the model answered. A run has exactly one of
// those, which is why it is a function rather than a record with a field
// per source and a rule about which may be set.
type Declaring func(Turn) Turn

// Misdeclaration is a rate at which this run corrupts what a write
// declares, in one direction. It holds one direction and not two, so a
// blended rate cannot be expressed.
//
// It is a property of the run and not of any frame: the corruption is
// applied where an arm hands a turn over, so no structure carries a clause
// for it and a frame cannot tell a corrupted declaration from an honest
// one. That is the point — it is what a frame would face from a writeback
// that is sometimes wrong.
type Misdeclaration struct {
	Direction string  `json:"direction"`
	Rate      float64 `json:"rate"`
	Seed      int64   `json:"seed"`
}

// applyTo returns the turn as a writeback with this error rate would have
// declared it.
func (m Misdeclaration) applyTo(t Turn) Turn {
	if !m.corrupts(t) {
		return t
	}
	if t.Declared == OperationJoin {
		t.Declared = OperationReplace
	} else {
		t.Declared = OperationJoin
	}
	return t
}

// corrupts reports whether this turn's declaration is one of the corrupted
// ones. The draw comes from the turn's own index rather than from a
// generator consumed in order, so a turn is corrupted identically for every
// frame, in every arm and whatever else the run does first: a difference
// between two frames measured under one rate is then a difference between
// the frames.
func (m Misdeclaration) corrupts(t Turn) bool {
	if t.Fact == nil || m.Rate <= 0 {
		return false
	}
	switch m.Direction {
	case directionJoinAsReplace:
		if t.Declared != OperationJoin {
			return false
		}
	case directionReplaceAsJoin:
		if t.Declared != OperationReplace {
			return false
		}
	default:
		return false
	}
	return uniform(m.Seed, t.Index) < m.Rate
}

// Flips are the addresses this rate corrupts and the year it corrupts them
// in, replayed over the workload rather than recorded during a run, so what
// the damage is measured against does not depend on a run having happened.
func (m Misdeclaration) Flips(w Workload) []Flip {
	var out []Flip
	for _, t := range w.Turns {
		if m.corrupts(t) {
			out = append(out, Flip{Address: t.Fact.Address, Year: t.Day/daysPerYear + 1})
		}
	}
	return out
}

// Flip is one corrupted declaration: where it landed and when.
type Flip struct {
	Address Address `json:"address"`
	Year    int     `json:"year"`
}

// Damage is what one direction at one rate did to the addresses it hit.
//
// Healed is the share of those addresses answering correctly again at the
// last year the workload asks them. Years is how many checkpoints an
// address carried the damage before answering correctly, counting to the
// end of the span where it never did. The two together separate a structure
// whose errors are repaired by the next write from one whose errors are
// permanent, which a year-ten recall figure alone cannot.
type Damage struct {
	Errors    int    `json:"errors"`
	Addresses int    `json:"addresses_hit"`
	Healed    Figure `json:"healed"`
	Years     Figure `json:"years_damaged"`
}

// MeasureDamage reads the corrupted addresses against what the frame
// answered at every year afterwards.
func MeasureDamage(flips []Flip, probes []ProbeResult, years int) Damage {
	first := map[Address]int{}
	order := make([]Address, 0, len(flips))
	for _, f := range flips {
		if at, seen := first[f.Address]; !seen || f.Year < at {
			if !seen {
				order = append(order, f.Address)
			}
			first[f.Address] = f.Year
		}
	}
	correct := map[Address]map[int]bool{}
	for _, p := range probes {
		address := Address{Subject: p.Subject, Attribute: p.Attribute}
		if correct[address] == nil {
			correct[address] = map[int]bool{}
		}
		correct[address][p.Year] = p.Found == p.Expected && p.Stale == 0
	}

	out := Damage{Errors: len(flips), Addresses: len(order)}
	var healed, carried []float64
	for _, address := range order {
		answers := correct[address]
		last, asked := 0, false
		for year := first[address]; year <= years; year++ {
			if _, ok := answers[year]; ok {
				last, asked = year, true
			}
		}
		if !asked {
			// The address is never asked again after it was corrupted, so
			// there is nothing to heal and nothing to carry.
			continue
		}
		healed = append(healed, boolAsFloat(answers[last]))
		carried = append(carried, float64(damagedYears(answers, first[address], years)))
	}
	out.Healed = NewFigure(healed)
	out.Years = NewFigure(carried)
	return out
}

// damagedYears counts the checkpoints from the corruption to the first one
// answered correctly, or to the end of the span when none is.
func damagedYears(answers map[int]bool, from, years int) int {
	var carried int
	for year := from; year <= years; year++ {
		right, asked := answers[year]
		if !asked {
			continue
		}
		if right {
			return carried
		}
		carried++
	}
	return carried
}

func boolAsFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// uniform maps a seed and a turn index to a value in [0,1). It is a hash
// rather than a generator so that nothing about the order a run does its
// work in can reach the result.
func uniform(seed int64, index int) float64 {
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[0:], uint64(seed))
	binary.LittleEndian.PutUint64(buf[8:], uint64(index))
	h := fnv.New64a()
	_, _ = h.Write(buf[:])
	return float64(h.Sum64()>>11) / float64(uint64(1)<<53)
}

// sweepRates are the error rates each direction is measured at. The low end
// sits below where any model plausibly is and the high end is far past
// usable, so the curve's shape — where it is flat and where it turns — is
// what the table shows rather than a row of round numbers.
var sweepRates = []float64{0.005, 0.01, 0.02, 0.05, 0.10, 0.20, 0.50}

// SweepResult is one direction at one rate over the whole decade. Eligible
// is how many writes declare the operation this direction corrupts, which
// is the denominator the rate applies to and the resolution the workload
// can show: a rate finer than one over that count corrupts nothing.
type SweepResult struct {
	Direction string       `json:"direction"`
	Rate      float64      `json:"rate"`
	Eligible  int          `json:"eligible_writes"`
	Damage    Damage       `json:"damage"`
	Years     []YearResult `json:"years"`
}

// MeasureSweep runs the declared-operation frame at every rate in each
// direction. It is that frame alone because it is the only one that reads a
// declaration: what a wrong declaration costs is a question about the
// structure whose answer depends on one.
//
// The oracle arm alone, for the same reason — the text arm carries no
// declaration, so there is nothing there to be wrong.
func MeasureSweep(ctx context.Context, w Workload, seed int64) ([]SweepResult, error) {
	var out []SweepResult
	for _, direction := range directionNames {
		for _, rate := range sweepRates {
			errs := Misdeclaration{Direction: direction, Rate: rate, Seed: seed}
			measured, err := Measure(ctx, w, frameAttributeDeclared, armOracle, NewEmbedCounter(), errs.applyTo)
			if err != nil {
				return nil, fmt.Errorf("%s at %v: %w", direction, rate, err)
			}
			out = append(out, SweepResult{
				Direction: direction,
				Rate:      rate,
				Eligible:  eligibleWrites(w, direction),
				Damage:    MeasureDamage(errs.Flips(w), measured.Probes, w.Params.Years),
				Years:     measured.Years,
			})
		}
	}
	return out, nil
}

// eligibleWrites counts the writes a direction can corrupt at all.
func eligibleWrites(w Workload, direction string) int {
	want := OperationJoin
	if direction == directionReplaceAsJoin {
		want = OperationReplace
	}
	var n int
	for _, t := range w.Turns {
		if t.Fact != nil && t.Declared == want {
			n++
		}
	}
	return n
}
