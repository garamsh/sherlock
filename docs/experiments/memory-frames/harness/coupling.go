package main

// Whether a write can reach what a read cannot.
//
// A store that finds what it supersedes by similarity has to retrieve the
// old value before it can end it: what the lookup does not surface survives
// the correction. A store that computes an address from the sentence writes
// there whether or not that address was ever returned to anyone. If that
// holds, reading and updating are coupled in one design and decoupled in
// the other, and the difference is larger than any figure this record
// carries — so it is measured here rather than argued.

// CouplingCell is one side of the comparison: the corrections whose
// superseded value a read had already returned, or had not.
type CouplingCell struct {
	Corrections int    `json:"corrections"`
	Applied     int    `json:"applied"`
	Rate        Figure `json:"applied_rate"`
}

// Coupling is whether a correction lands when the value it corrects was
// never in any context before it. Equal rates mean the write does not
// depend on the read; a gap means it does.
type Coupling struct {
	Reached   CouplingCell `json:"reached_before"`
	Unreached CouplingCell `json:"never_reached_before"`
}

// correction is one value ending at one year on one thread.
type correction struct {
	subject   string
	attribute string
	value     string
	year      int
}

// corrections reads the workload for every value a later assertion ends.
// The value that returns to a thread it once left is not a correction of
// itself, so a reverted thread contributes the one value it truly ends.
func corrections(w Workload) []correction {
	var out []correction
	for _, thread := range w.Threads {
		var live []string
		for _, a := range thread.Asserts {
			if a.Replaces {
				for _, ended := range live {
					if ended == a.Value {
						continue
					}
					out = append(out, correction{subject: thread.Subject,
						attribute: thread.Attribute, value: ended, year: a.Year})
				}
				live = nil
			}
			if !contains(live, a.Value) {
				live = append(live, a.Value)
			}
		}
	}
	return out
}

// MeasureCoupling splits every correction by whether a read had returned
// the value it ends before it landed, and scores each side on whether the
// correction took.
//
// A correction has taken when the structure no longer holds the value it
// ended. That is read off what the structure holds rather than off what it
// returned, because a value a read never surfaces is absent from a context
// whether or not anything corrected it — scoring on the context would count
// unreachable as corrected, which is the opposite of what is being asked.
//
// What stands in for the write's own lookup is whether any read had
// returned the value before the correction landed. They are not the same
// retrieval, but they are the same ranking over the same store, and the
// harness cannot see inside a write.
func MeasureCoupling(w Workload, heldAt map[int]map[string]bool, firstReached map[string]int) Coupling {
	var out Coupling
	var reached, unreached []float64
	for _, c := range corrections(w) {
		applied := !heldAt[c.year][c.value]
		at, everReached := firstReached[c.value]
		if everReached && at < c.year {
			out.Reached.Corrections++
			if applied {
				out.Reached.Applied++
			}
			reached = append(reached, boolAsFloat(applied))
			continue
		}
		out.Unreached.Corrections++
		if applied {
			out.Unreached.Applied++
		}
		unreached = append(unreached, boolAsFloat(applied))
	}
	out.Reached.Rate = NewFigure(reached)
	out.Unreached.Rate = NewFigure(unreached)
	return out
}
