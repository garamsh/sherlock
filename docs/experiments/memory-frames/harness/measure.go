package main

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// The two arms. They differ in one thing: whether a turn reaches a frame
// with the assertion it carries already extracted.
//
// Extraction is held constant across every frame rather than left to each
// one, because with no model in the run a frame that addresses facts by
// subject and attribute would have to parse sentences, and the comparison
// would become a comparison of parsers instead of structures. The oracle
// arm removes that variable; the text arm is what a frame faces without it,
// and the gap between the two is what extraction is worth.
//
// The oracle says what was asserted and nothing more. It does not say
// whether an attribute holds one value or several, and it does not say what
// an assertion replaces: those are the questions being asked.
const (
	armOracle = "oracle"
	armText   = "text"
)

// armNames is the order every result file and table reports the arms in.
var armNames = []string{armOracle, armText}

// ProbeResult is one thread asked at the end of one year.
type ProbeResult struct {
	Year      int    `json:"year"`
	Class     string `json:"class"`
	Subject   string `json:"subject"`
	Attribute string `json:"attribute"`
	// Expected is how many values were true at the checkpoint and Found how
	// many of them the frame put in the context.
	Expected int `json:"expected"`
	Found    int `json:"found"`
	// Superseded is how many values had been true and were not, and Stale
	// reports whether the frame returned any of them.
	Superseded   int `json:"superseded"`
	Stale        int `json:"stale_returned"`
	ContextChars int `json:"context_chars"`
	// SubjectRelated is how many values are live anywhere on the probed
	// subject, and SubjectReached how many of them the context carried. A
	// frame answering the question while leaving the rest of the subject
	// outside the context leaves that rest where no writeback can reach it.
	SubjectRelated int `json:"subject_related"`
	SubjectReached int `json:"subject_reached"`
	// Scanned is how much of what the frame holds it read to answer this
	// question, in the unit its Size reports.
	Scanned int `json:"scanned"`
}

// ClassResult is one fact class at one year.
type ClassResult struct {
	Class  string `json:"class"`
	Recall Figure `json:"recall"`
	Stale  Figure `json:"stale_returned"`
}

// YearResult is one checkpoint: every thread answerable that year, reported
// as a whole and split by class. A decade total would hide which year
// failed, which is the question.
type YearResult struct {
	Year         int           `json:"year"`
	Probes       int           `json:"probes"`
	Recall       Figure        `json:"recall"`
	Stale        Figure        `json:"stale_returned"`
	ContextChars Figure        `json:"context_chars"`
	Scanned      Figure        `json:"scanned"`
	Classes      []ClassResult `json:"classes"`
}

// SizeAt is what a frame held at the end of one year.
type SizeAt struct {
	Year int  `json:"year"`
	Size Size `json:"size"`
}

// FrameResult is one frame measured in one arm over the whole decade.
type FrameResult struct {
	Frame  string        `json:"frame"`
	Arm    string        `json:"arm"`
	Years  []YearResult  `json:"years"`
	Sizes  []SizeAt      `json:"sizes"`
	Probes []ProbeResult `json:"probes"`
	// Reach is what this frame could not reach, per year, and what holding
	// it costs. Coupling is whether a correction lands on a value no read
	// ever returned.
	Reach    []YearReach `json:"reach"`
	Coupling Coupling    `json:"coupling"`
}

// SizeYears are the years the store size is reported at: the first, the
// middle and the last.
func SizeYears(years int) []int {
	return []int{1, (years + 1) / 2, years}
}

// Measure runs one frame over the whole workload in one arm, asking every
// answerable thread at the end of every year.
func Measure(ctx context.Context, w Workload, frameName, arm string, counter *EmbedCounter,
	declaring Declaring) (FrameResult, error) {
	dir, err := os.MkdirTemp("", "memory-frames")
	if err != nil {
		return FrameResult{}, fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	frame, err := NewFrame(frameName, dir, counter)
	if err != nil {
		return FrameResult{}, err
	}
	defer frame.Close()

	out := FrameResult{Frame: frameName, Arm: arm}
	sizeYears := SizeYears(w.Params.Years)
	planted := plantedValues(w)
	// firstReached is the first year a value appeared in any context. A
	// value missing from it at the end of the run was never reached, and a
	// value whose first year is after a correction was not reachable when
	// that correction landed.
	firstReached := map[string]int{}
	// heldAt is the planted values the structure holds at each checkpoint,
	// which is what says whether a correction took.
	heldAt := map[int]map[string]bool{}
	year := 1
	checkpoint := func() error {
		probes, injected, size, err := ask(ctx, frame, w, year, arm, declaring, planted)
		if err != nil {
			return err
		}
		for _, values := range injected {
			for _, value := range values {
				if _, seen := firstReached[value]; !seen {
					firstReached[value] = year
				}
			}
		}
		out.Probes = append(out.Probes, probes...)
		out.Years = append(out.Years, summarizeYear(year, probes))
		if containsInt(sizeYears, year) {
			out.Sizes = append(out.Sizes, SizeAt{Year: year, Size: size})
		}
		held, err := frame.Held(ctx)
		if err != nil {
			return fmt.Errorf("held at year %d: %w", year, err)
		}
		heldAt[year] = map[string]bool{}
		for _, text := range held {
			for _, value := range valuesIn(text, planted) {
				heldAt[year][value] = true
			}
		}
		out.Reach = append(out.Reach, summarizeReach(w, year, probes, heldAt[year], firstReached, size))
		year++
		return nil
	}

	for _, turn := range w.Turns {
		for year <= w.Params.Years && turn.Day >= year*daysPerYear {
			if err := checkpoint(); err != nil {
				return FrameResult{}, err
			}
		}
		if err := frame.Observe(ctx, observed(turn, arm, declaring)); err != nil {
			return FrameResult{}, err
		}
	}
	for year <= w.Params.Years {
		if err := checkpoint(); err != nil {
			return FrameResult{}, err
		}
	}
	out.Coupling = MeasureCoupling(w, heldAt, firstReached)
	return out, nil
}

// summarizeReach reads one checkpoint against the workload's own relations
// and against everything the frame has reached so far.
func summarizeReach(w Workload, year int, probes []ProbeResult, held map[string]bool,
	firstReached map[string]int, size Size) YearReach {
	out := YearReach{Year: year, Units: size.Units,
		ContentBytes: size.ContentBytes, StoredBytes: size.StoredBytes}
	var thread, subject []float64
	for _, p := range probes {
		thread = append(thread, ratio(p.Found, p.Expected))
		subject = append(subject, ratio(p.SubjectReached, p.SubjectRelated))
	}
	out.ThreadReach = NewFigure(thread)
	out.SubjectReach = NewFigure(subject)

	var frozen []string
	for value := range held {
		out.Held++
		if _, reached := firstReached[value]; reached {
			out.Reached++
			continue
		}
		frozen = append(frozen, value)
	}
	out.Frozen = len(frozen)
	out.Share = ratio(out.Frozen, out.Held)
	needed := neededAfter(w, year)
	for _, value := range frozen {
		if needed[value] {
			out.NeededLater++
		}
	}
	return out
}

// observed is the turn as the arm delivers it. The oracle arm hands a frame
// the extracted forms beside the text; the text arm hands it the text
// alone. This is the one place an arm touches a turn, so a frame never
// learns which arm it is in.
func observed(t Turn, arm string, declaring Declaring) Turn {
	if arm == armText {
		t.Fact, t.Ask, t.Declared = nil, nil, ""
		return t
	}
	if declaring == nil {
		return t
	}
	return declaring(t)
}

// ask puts every thread answerable at this year to the frame and scores
// what comes back. A checkpoint reads the frame and never writes to it.
func ask(ctx context.Context, frame Frame, w Workload, year int, arm string,
	declaring Declaring, planted map[string]bool) ([]ProbeResult, [][]string, Size, error) {
	var (
		probes  []ProbeResult
		carried [][]string
	)
	bySubject := liveBySubject(w, year)
	for _, thread := range w.Threads {
		if !thread.ProbedAt(year) {
			continue
		}
		injected, err := frame.Context(ctx, observed(Checkpoint(year, thread), arm, declaring))
		if err != nil {
			return nil, nil, Size{}, fmt.Errorf("context for %s at year %d: %w", thread.Subject, year, err)
		}
		live, stale := thread.TruthAt(year)
		result := ProbeResult{
			Year: year, Class: thread.Class, Subject: thread.Subject, Attribute: thread.Attribute,
			Expected: len(live), Superseded: len(stale),
			ContextChars: len(injected.Text), Scanned: injected.Scanned,
		}
		for _, value := range live {
			if Mentions(injected.Text, value) {
				result.Found++
			}
		}
		for _, value := range stale {
			if Mentions(injected.Text, value) {
				result.Stale = 1
				break
			}
		}
		values := valuesIn(injected.Text, planted)
		result.SubjectRelated = len(bySubject[thread.Subject])
		for _, value := range bySubject[thread.Subject] {
			if contains(values, value) {
				result.SubjectReached++
			}
		}
		probes = append(probes, result)
		carried = append(carried, values)
	}
	size, err := frame.Size(ctx)
	if err != nil {
		return nil, nil, Size{}, fmt.Errorf("size at year %d: %w", year, err)
	}
	return probes, carried, size, nil
}

// summarizeYear turns one checkpoint's probes into the figures the record
// reports, as a whole and per class.
func summarizeYear(year int, probes []ProbeResult) YearResult {
	out := YearResult{Year: year, Probes: len(probes)}
	var recall, stale, chars, scanned []float64
	for _, p := range probes {
		recall = append(recall, ratio(p.Found, p.Expected))
		chars = append(chars, float64(p.ContextChars))
		scanned = append(scanned, float64(p.Scanned))
		if p.Superseded > 0 {
			stale = append(stale, float64(p.Stale))
		}
	}
	out.Recall = NewFigure(recall)
	out.Stale = NewFigure(stale)
	out.ContextChars = NewFigure(chars)
	out.Scanned = NewFigure(scanned)
	for _, class := range classNames {
		var classRecall, classStale []float64
		for _, p := range probes {
			if p.Class != class {
				continue
			}
			classRecall = append(classRecall, ratio(p.Found, p.Expected))
			if p.Superseded > 0 {
				classStale = append(classStale, float64(p.Stale))
			}
		}
		if len(classRecall) == 0 {
			continue
		}
		out.Classes = append(out.Classes, ClassResult{Class: class,
			Recall: NewFigure(classRecall), Stale: NewFigure(classStale)})
	}
	return out
}

// Mentions reports whether the injected context carries the value. The
// match is on the whole hyphenated pair with non-word characters on both
// sides, so a value is never credited to a longer token that contains it —
// the rule #371 had to add a word boundary for.
func Mentions(injected, value string) bool {
	haystack, needle := strings.ToLower(injected), strings.ToLower(value)
	for i := 0; i+len(needle) <= len(haystack); {
		j := strings.Index(haystack[i:], needle)
		if j < 0 {
			return false
		}
		start := i + j
		if isBoundary(haystack, start-1) && isBoundary(haystack, start+len(needle)) {
			return true
		}
		i = start + 1
	}
	return false
}

// isBoundary reports whether the byte at i ends a value token, the ends of
// the text included.
func isBoundary(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return true
	}
	return !isValueByte(s[i])
}

// isValueByte reports whether a byte can be part of a value. A hyphen can,
// because a value is a hyphenated pair and a longer hyphenated token is not
// the value it contains. The haystack is lower-cased before the match, so
// no upper-case letter reaches here.
func isValueByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-'
}

func ratio(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole)
}

func containsInt(values []int, want int) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
