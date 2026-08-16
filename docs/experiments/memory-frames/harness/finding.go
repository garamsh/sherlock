package main

import (
	"context"
	"fmt"
	"os"

	"github.com/garamsh/gagent/internal/memory"
)

// Every addressed figure this record published was produced with the
// question already turned into (subject, attribute). A frame handed the
// address is measured on holding and updating and never on finding, which
// is the half of the standard that asks a memory to surface the meaningful
// one. This study is the run in which no frame receives the address unless
// the form it is measured under supplies it.
//
// The write arm and the question form are separate axes here. The arm
// decides what a turn carries when it is observed; the form decides what a
// question carries when it is asked. Every earlier run tied the two
// together — the text arm stripped the address from the question because it
// stripped it from everything — which is why an addressed store had either
// perfect extraction and a perfect question or neither.

// The forms a question reaches a frame in. They are the read side of the
// bracket the write side already has: an oracle end, an end with nothing,
// and the buildable middle where a model reads the words.
const (
	// questionAddressed hands over the address the question was generated
	// from. It is the ceiling every committed figure was measured at.
	questionAddressed = "addressed"
	// questionWorded hands over the words and nothing else. It is the floor,
	// and it is what a structure faces from a question a system cannot
	// address for it.
	questionWorded = "worded"
	// questionResolved hands over the address a model read out of the words.
	// It is the read-side twin of the extracted arm, and it is where a
	// buildable design sits.
	questionResolved = "resolved"
)

// questionForms is the order every result file and table reports the forms
// in.
var questionForms = []string{questionAddressed, questionWorded, questionResolved}

// subjectQuestion is the multi-address question. Every checkpoint before
// this study asked about one address; this one asks about a person, which
// is answered from every address under them.
func subjectQuestion(subject string) string {
	return fmt.Sprintf("What do you know about %s?", subject)
}

// SubjectResult is one subject asked at one year.
//
// Related is every value true anywhere on that subject, which is the
// relation §What a read cannot reach already defined and read off the
// workload rather than off any retrieval function. Addresses is how many
// places that relation is spread over, which is what separates this
// question from every earlier one.
type SubjectResult struct {
	Year      int    `json:"year"`
	Subject   string `json:"subject"`
	Addresses int    `json:"addresses"`
	Related   int    `json:"related"`
	Reached   int    `json:"reached"`
	// Superseded is how many values on the subject have stopped being true,
	// and Stale reports whether the answer carried any of them.
	Superseded   int `json:"superseded"`
	Stale        int `json:"stale_returned"`
	ContextChars int `json:"context_chars"`
	Scanned      int `json:"scanned"`
}

// SubjectYear is every subject asked at one year.
//
// Fits is the share of answers inside the read budget. An answer that is
// empty fits and answers nothing, so it is never read without the recall
// beside it.
type SubjectYear struct {
	Year         int    `json:"year"`
	Asked        int    `json:"asked"`
	Recall       Figure `json:"recall"`
	Stale        Figure `json:"stale_returned"`
	Addresses    Figure `json:"addresses_spanned"`
	ContextChars Figure `json:"context_chars"`
	Scanned      Figure `json:"scanned"`
	Fits         Figure `json:"fits_the_budget"`
}

// FindingFrame is one frame measured under one arm and one question form:
// the single-address questions every earlier round asked, and the
// multi-address question this one adds.
type FindingFrame struct {
	Frame    string `json:"frame"`
	Arm      string `json:"arm"`
	Question string `json:"question"`
	// Budget is the read budget the frame answered inside, in tokens. Zero
	// is the shipped default, which is what every figure outside the sweep
	// was measured at.
	Budget   int           `json:"budget"`
	Years    []YearResult  `json:"years,omitempty"`
	Subjects []SubjectYear `json:"subjects,omitempty"`
}

// FindingStudy is the whole finding run. It is its own file for the reason
// the shape study is: it asks a different question of the same workload,
// and the committed decade figures stay exactly what they were.
type FindingStudy struct {
	Seed   int64  `json:"seed"`
	Digest string `json:"turn_digest"`
	Params Params `json:"params"`
	// Budget is the read budget the unswept rows answered inside, in tokens.
	Budget     int              `json:"budget"`
	Resolution *ResolutionScore `json:"resolution,omitempty"`
	Frames     []FindingFrame   `json:"frames"`
	// Budgets is the multi-address question swept over read budgets. #472
	// found an addressed store insensitive to that sweep because it returns
	// the one address it was asked for; a question spanning many addresses
	// is where that property is tested.
	Budgets []FindingFrame `json:"budgets"`
}

// MeasureFinding runs every frame over the decade in every arm and every
// form, and sweeps the multi-address question over the read budget.
//
// The sweep is the oracle arm and the addressed form alone: it is asking
// what a structure returns when what is relevant does not fit, and a
// structure holding nothing has nothing that does not fit.
func MeasureFinding(ctx context.Context, w Workload, resolving Resolving) (FindingStudy, error) {
	out := FindingStudy{Seed: w.Seed, Digest: w.TurnDigest, Params: w.Params,
		Budget: memory.DefaultRecallTokenBudget}
	forms := questionForms
	if resolving == nil {
		forms = []string{questionAddressed, questionWorded}
	}
	for _, arm := range armNames {
		for _, form := range forms {
			for _, frame := range frameNames {
				row, err := find(ctx, w, frame, arm, form, 0, resolving, true)
				if err != nil {
					return FindingStudy{}, err
				}
				out.Frames = append(out.Frames, row)
				fmt.Printf("%s, %s arm, %s question: %d years\n", frame, arm, form, len(row.Years))
			}
		}
	}
	for _, budget := range sweptBudgets {
		for _, frame := range frameNames {
			row, err := find(ctx, w, frame, armOracle, questionAddressed, budget, resolving, false)
			if err != nil {
				return FindingStudy{}, err
			}
			out.Budgets = append(out.Budgets, row)
		}
	}
	fmt.Printf("finding: %d rows, %d of them swept over the read budget\n", len(out.Frames), len(out.Budgets))
	return out, nil
}

// find measures one frame over the decade under one arm and one form.
//
// The two kinds of question are asked in separate runs over separate
// stores, because a read is not always free: the shipped store records an
// access on every recall, so asking both out of one store would answer the
// second kind from a store the first had already moved.
func find(ctx context.Context, w Workload, frameName, arm, form string, budget int,
	resolving Resolving, threads bool) (FindingFrame, error) {
	out := FindingFrame{Frame: frameName, Arm: arm, Question: form, Budget: budget}
	if threads {
		years, err := findThreads(ctx, w, frameName, arm, form, budget, resolving)
		if err != nil {
			return FindingFrame{}, err
		}
		out.Years = years
	}
	subjects, err := findSubjects(ctx, w, frameName, arm, form, budget, resolving)
	if err != nil {
		return FindingFrame{}, err
	}
	out.Subjects = subjects
	return out, nil
}

// findThreads asks every answerable thread its own question at the end of
// every year, which is the question every earlier round asked.
//
// It scores a probe the way the decade run's own checkpoint does rather
// than through it: that path also takes a size, a reach and a bound this
// study has no use for, and leaving it untouched is what keeps the rows it
// committed from moving. `finding_test.go` reads the two files against each
// other, so the two paths cannot drift.
func findThreads(ctx context.Context, w Workload, frameName, arm, form string, budget int,
	resolving Resolving) ([]YearResult, error) {
	var out []YearResult
	err := walk(ctx, w, frameName, arm, budget, func(frame Frame, year int) error {
		var probes []ProbeResult
		for _, thread := range w.Threads {
			if !thread.ProbedAt(year) {
				continue
			}
			turn := Checkpoint(year, thread)
			turn.Ask = addressFor(turn.Text, turn.Ask, form, resolving)
			injected, err := frame.Context(ctx, turn)
			if err != nil {
				return fmt.Errorf("context for %s at year %d: %w", thread.Subject, year, err)
			}
			live, stale := thread.TruthAt(year)
			probe := ProbeResult{
				Year: year, Class: thread.Class, Subject: thread.Subject, Attribute: thread.Attribute,
				Expected: len(live), Superseded: len(stale),
				ContextChars: len(injected.Text), Scanned: injected.Scanned,
			}
			for _, value := range live {
				if Mentions(injected.Text, value) {
					probe.Found++
				}
			}
			for _, value := range stale {
				if Mentions(injected.Text, value) {
					probe.Stale = 1
					break
				}
			}
			probes = append(probes, probe)
		}
		out = append(out, summarizeYear(year, probes))
		return nil
	})
	return out, err
}

// findSubjects asks every subject the multi-address question at the end of
// every year.
func findSubjects(ctx context.Context, w Workload, frameName, arm, form string, budget int,
	resolving Resolving) ([]SubjectYear, error) {
	chars := budgetChars(budget)
	planted := plantedValues(w)
	var out []SubjectYear
	err := walk(ctx, w, frameName, arm, budget, func(frame Frame, year int) error {
		live, stale := subjectTruth(w, year)
		var results []SubjectResult
		for _, subject := range subjects {
			related := live[subject]
			if len(related) == 0 {
				continue
			}
			turn := CheckpointAt(year, subjectQuestion(subject))
			turn.Ask = addressFor(turn.Text, &Address{Subject: subject}, form, resolving)
			injected, err := frame.Context(ctx, turn)
			if err != nil {
				return fmt.Errorf("context for %s at year %d: %w", subject, year, err)
			}
			returned := valuesIn(injected.Text, planted)
			result := SubjectResult{
				Year: year, Subject: subject, Addresses: addressesOn(w, subject, year),
				Related: len(related), Superseded: len(stale[subject]),
				ContextChars: len(injected.Text), Scanned: injected.Scanned,
			}
			for _, value := range related {
				if contains(returned, value) {
					result.Reached++
				}
			}
			for _, value := range stale[subject] {
				if contains(returned, value) {
					result.Stale = 1
					break
				}
			}
			results = append(results, result)
		}
		out = append(out, summarizeSubjects(year, results, chars))
		return nil
	})
	return out, err
}

// walk observes the decade through one fresh frame, calling at() at the end
// of every year. It is the finding run's own loop rather than Measure's
// because a finding run scores two kinds of question and takes no size,
// no reach and no bound.
func walk(ctx context.Context, w Workload, frameName, arm string, budget int,
	at func(frame Frame, year int) error) error {
	dir, err := os.MkdirTemp("", "memory-frames-finding")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(dir)
	frame, err := NewFrame(frameName, dir, NewEmbedCounter(), budget)
	if err != nil {
		return err
	}
	defer frame.Close()

	year := 1
	for _, turn := range w.Turns {
		for year <= w.Params.Years && turn.Day >= year*daysPerYear {
			if err := at(frame, year); err != nil {
				return err
			}
			year++
		}
		if err := frame.Observe(ctx, observed(turn, arm, nil)); err != nil {
			return fmt.Errorf("observe turn %d: %w", turn.Index, err)
		}
	}
	for year <= w.Params.Years {
		if err := at(frame, year); err != nil {
			return err
		}
		year++
	}
	return nil
}

// addressFor is the address a question reaches a frame with. It is the one
// place a form touches a question, so a frame never learns which form it is
// under.
func addressFor(text string, address *Address, form string, resolving Resolving) *Address {
	switch form {
	case questionAddressed:
		return address
	case questionResolved:
		return resolving(text)
	default:
		return nil
	}
}

// summarizeSubjects turns one checkpoint's multi-address questions into the
// figures the record reports.
func summarizeSubjects(year int, results []SubjectResult, budget int) SubjectYear {
	out := SubjectYear{Year: year, Asked: len(results)}
	var recall, stale, addresses, chars, scanned, fits []float64
	for _, r := range results {
		recall = append(recall, ratio(r.Reached, r.Related))
		if r.Superseded > 0 {
			stale = append(stale, float64(r.Stale))
		}
		addresses = append(addresses, float64(r.Addresses))
		chars = append(chars, float64(r.ContextChars))
		scanned = append(scanned, float64(r.Scanned))
		fits = append(fits, boolAsFloat(r.ContextChars <= budget))
	}
	out.Recall = NewFigure(recall)
	out.Stale = NewFigure(stale)
	out.Addresses = NewFigure(addresses)
	out.ContextChars = NewFigure(chars)
	out.Scanned = NewFigure(scanned)
	out.Fits = NewFigure(fits)
	return out
}

// subjectTruth is what is true anywhere on each subject at one year, and
// what was true on it and is not. It is the multi-address question's ground
// truth, read off the workload for the reason every relation in this record
// is: one defined by a retrieval function would measure that function.
func subjectTruth(w Workload, year int) (live, stale map[string][]string) {
	live, stale = map[string][]string{}, map[string][]string{}
	for _, thread := range w.Threads {
		threadLive, threadStale := thread.TruthAt(year)
		live[thread.Subject] = append(live[thread.Subject], threadLive...)
		stale[thread.Subject] = append(stale[thread.Subject], threadStale...)
	}
	for subject, values := range stale {
		kept := values[:0]
		for _, value := range values {
			if !contains(live[subject], value) {
				kept = append(kept, value)
			}
		}
		stale[subject] = kept
	}
	return live, stale
}

// addressesOn is how many of a subject's addresses hold something true at
// one year, which is how many places an answer about that subject has to
// come from.
func addressesOn(w Workload, subject string, year int) int {
	var out int
	for _, thread := range w.Threads {
		if thread.Subject != subject {
			continue
		}
		if live, _ := thread.TruthAt(year); len(live) > 0 {
			out++
		}
	}
	return out
}

// budgetChars is a read budget in the characters a context is counted in.
// Zero takes the shipped default, which is what every figure outside the
// sweep was measured at.
func budgetChars(tokens int) int {
	if tokens <= 0 {
		return documentLimit
	}
	return tokens * memory.CharsPerToken
}
