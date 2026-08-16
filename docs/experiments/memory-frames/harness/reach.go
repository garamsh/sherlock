package main

import "strings"

// What a structure answers has been measured every round. What it cannot
// reach has not, and the two are different questions: a memory the read
// never returns is not merely unread, it is frozen, because the writeback
// only ever sees what arrived in the context. A fact outside every context
// for a decade stays whatever it was, right or wrong, and no recall figure
// says so.
//
// Related is read off the workload's ground truth and never off similarity.
// The workload knows which values are true, which thread they sit on and
// which subject they belong to; a relation defined by a retrieval function
// would measure that function instead of the structure, which is the trap
// this programme exists to avoid.

// YearReach is one frame at one year: how much of what was related reached
// the context, how much of what it holds no read has ever reached, and what
// it costs to hold. The size trajectory sits here rather than beside the
// sizes already committed, so that those stay exactly as they were.
type YearReach struct {
	Year int `json:"year"`
	// ThreadReach is the share of the values live on the probed thread that
	// the context carried. It is recall under another name and is here so
	// the two reaches can be read against each other.
	ThreadReach Figure `json:"thread_reach"`
	// SubjectReach is the share of the values live anywhere on the probed
	// subject that the context carried. A structure that answers the
	// question and brings nothing else leaves the rest of that subject
	// outside every writeback that turn could have made.
	SubjectReach Figure `json:"subject_reach"`
	// Held, Reached and Frozen count planted values: what the structure
	// holds at this year, how many of them any context has carried by now,
	// and the difference — the part of the store no write can correct.
	Held    int     `json:"held"`
	Reached int     `json:"reached"`
	Frozen  int     `json:"frozen"`
	Share   float64 `json:"frozen_share"`
	// NeededLater is how many of the frozen values a later checkpoint still
	// requires. It is what forgetting the never-reached would cost, and it
	// is zero when the rule is free.
	NeededLater int `json:"frozen_needed_later"`
	// Units and bytes, per year rather than at three of them, which is what
	// says whether a decade approaches any ceiling.
	Units        int   `json:"units"`
	ContentBytes int64 `json:"content_bytes"`
	StoredBytes  int64 `json:"stored_bytes"`
}

// plantedValues is every value the workload ever asserts, which is the
// vocabulary a context is read against.
func plantedValues(w Workload) map[string]bool {
	out := map[string]bool{}
	for _, thread := range w.Threads {
		for _, a := range thread.Asserts {
			out[a.Value] = true
		}
	}
	return out
}

// valuesIn returns the planted values a text carries. It splits on
// everything a value cannot contain, so a value is found exactly where the
// credit rule finds it and nowhere a longer token merely contains it.
func valuesIn(text string, planted map[string]bool) []string {
	var out []string
	for _, token := range strings.FieldsFunc(strings.ToLower(text), notValueRune) {
		if planted[token] && !contains(out, token) {
			out = append(out, token)
		}
	}
	return out
}

func notValueRune(r rune) bool { return !isValueRune(r) }

// isValueRune reports whether a rune can be part of a value. It is the
// rune-wise twin of the byte test the credit rule uses, so a value is found
// in a context exactly where a probe would credit it.
func isValueRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-'
}

// neededAfter is every value some checkpoint after the given year still
// requires. Forgetting anything in this set costs recall; forgetting
// anything outside it costs nothing any measurement here can detect.
func neededAfter(w Workload, year int) map[string]bool {
	out := map[string]bool{}
	for later := year + 1; later <= w.Params.Years; later++ {
		for _, thread := range w.Threads {
			if !thread.ProbedAt(later) {
				continue
			}
			live, _ := thread.TruthAt(later)
			for _, value := range live {
				out[value] = true
			}
		}
	}
	return out
}
