package main

import (
	"fmt"
	"math/rand"
	"sort"
	"time"
)

// The fact classes. Each names a way a fact behaves over years, and each is
// reported on its own line at every year, because a decade total hides
// which of them failed.
const (
	// classCarried is asserted once in the first year and never mentioned
	// again. It must still be answerable in the last year.
	classCarried = "carried"
	// classReplaced is asserted, replaced, and replaced again. Only the
	// latest value may come back.
	classReplaced = "replaced"
	// classReverted is asserted, replaced, and then returned to the value
	// it started with. The value that must come back is one the thread
	// already superseded once.
	classReverted = "reverted"
	// classParallel is two values of one attribute, both true, asserted
	// years apart and never contradicting.
	classParallel = "parallel"
	// classRefined is one value and then a narrower statement of the same
	// attribute that does not contradict it. It is the case a per-case
	// rule was written for once, so it is the case that says whether a
	// structure earned its score.
	classRefined = "refined"
	// classFresh is asserted in the year it is asked. It is the control
	// that says whether a frame holds what it has just seen.
	classFresh = "fresh"
)

// classNames is the order every table reports the classes in.
var classNames = []string{classCarried, classReplaced, classReverted, classParallel, classRefined, classFresh}

// daysPerYear is the year the simulated calendar uses. It is a plain 365
// with no leap correction: nothing measured here reads a date, only an
// elapsed duration, and a uniform year keeps the checkpoint arithmetic
// exact.
const daysPerYear = 365

// The schedule points, as fractions of the span, so the workload keeps its
// shape at a span other than ten years. At the default span they are years
// 1, 3 and 7 for a replacement and years 2 and 6 for a coexistence, which
// is the schedule issue #443 asks for.
const (
	scheduleMiddle    = 0.3
	scheduleLate      = 0.7
	scheduleEarlyPair = 0.2
	scheduleLatePair  = 0.6
)

// Params are the workload's shape. Every one of them is a flag reported in
// the run header: a span, a session count and a noise volume that nobody
// chose are three constants a later comparison cannot be read against.
type Params struct {
	Years           int `json:"years"`
	SessionsPerYear int `json:"sessions_per_year"`
	TurnsPerSession int `json:"turns_per_session"`
	ThreadsPerClass int `json:"threads_per_class"`
	// The counts below are what the parameters above produced, so a reader
	// can check the noise volume rather than compute it.
	Sessions   int `json:"sessions"`
	Turns      int `json:"turns"`
	FactTurns  int `json:"fact_turns"`
	NoiseTurns int `json:"noise_turns"`
	Threads    int `json:"threads"`
	Probes     int `json:"probes"`
}

// Assert is one assertion on a thread. Replaces is ground truth and no
// frame ever sees it: whether a new value ends the earlier ones or stands
// beside them is what the comparison is asking. Day is the day the
// scheduler placed it on, which is what the interval between two writes to
// one address is read from.
type Assert struct {
	Year int `json:"year"`
	Day  int `json:"day"`
	// Wording names the template the sentence was written from. It is
	// ground truth, like Replaces, and it is what an error rate is cut by
	// to say whether a mistake follows the words or falls anywhere.
	Wording  string `json:"wording"`
	Value    string `json:"value"`
	Text     string `json:"text"`
	Replaces bool   `json:"replaces"`
}

// Thread is one (subject, attribute) fact line over the decade.
type Thread struct {
	Class     string   `json:"class"`
	Subject   string   `json:"subject"`
	Attribute string   `json:"attribute"`
	Question  string   `json:"question"`
	Asserts   []Assert `json:"assertions"`
}

// Workload is the decade: the turns in order, the threads that seeded them,
// and the parameters that produced both. The turns are left out of the
// committed file — they are thousands of lines the seed reproduces — and
// TurnDigest is what stands in for them.
type Workload struct {
	Seed       int64    `json:"seed"`
	Params     Params   `json:"params"`
	Start      string   `json:"start"`
	TurnDigest string   `json:"turn_digest"`
	Threads    []Thread `json:"threads"`
	Turns      []Turn   `json:"-"`
}

// start is the day the decade begins. The clock is fixed rather than taken
// from the wall clock, because the ranking function's recency term and the
// retention window both read it.
var start = time.Date(2016, 1, 1, 9, 0, 0, 0, time.UTC)

// ProbedAt reports whether the thread is asked at the end of the given
// year. A fresh thread is asked only in its own year; every other thread is
// asked in every year from its first assertion on.
func (t Thread) ProbedAt(year int) bool {
	if len(t.Asserts) == 0 {
		return false
	}
	if t.Class == classFresh {
		return t.Asserts[0].Year == year
	}
	return t.Asserts[0].Year <= year
}

// TruthAt returns the values true at the end of the given year and the
// values that were true earlier and are not now. Both are in assertion
// order, so a result file reads the same on every run.
func (t Thread) TruthAt(year int) (live, superseded []string) {
	for _, a := range t.Asserts {
		if a.Year > year {
			break
		}
		if a.Replaces {
			superseded = append(superseded, live...)
			live = nil
		}
		live = append(live, a.Value)
	}
	// A reverted thread asserts a value it superseded earlier; it is live
	// now, so it is not also stale.
	out := superseded[:0]
	for _, v := range superseded {
		if !contains(live, v) {
			out = append(out, v)
		}
	}
	return live, out
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// GenerateWorkload builds the decade from a seed. It is deterministic in
// the seed and the parameters and in nothing else: every draw comes from
// one generator consumed in a fixed order, and no result depends on map
// iteration.
func GenerateWorkload(seed int64, p Params) (Workload, error) {
	if p.Years < 4 || p.SessionsPerYear < 1 || p.TurnsPerSession < 1 || p.ThreadsPerClass < 1 {
		return Workload{}, fmt.Errorf("years must be at least 4 and the other parameters positive")
	}
	rng := rand.New(rand.NewSource(seed))

	threads, err := buildThreads(rng, p)
	if err != nil {
		return Workload{}, err
	}
	turns, factTurns, err := schedule(rng, p, threads)
	if err != nil {
		return Workload{}, err
	}

	p.Sessions = p.Years * p.SessionsPerYear
	p.Turns = len(turns)
	p.FactTurns = factTurns
	p.NoiseTurns = len(turns) - factTurns
	p.Threads = len(threads)
	for year := 1; year <= p.Years; year++ {
		for _, t := range threads {
			if t.ProbedAt(year) {
				p.Probes++
			}
		}
	}
	return Workload{Seed: seed, Params: p, Start: start.Format(time.RFC3339),
		TurnDigest: turnDigest(turns), Threads: threads, Turns: turns}, nil
}

// buildThreads draws every thread's subject, attribute and values, and
// writes the sentence each assertion is said in.
func buildThreads(rng *rand.Rand, p Params) ([]Thread, error) {
	n := p.ThreadsPerClass
	wanted := 5*n + p.Years*n
	pairs := make([][2]int, 0, len(subjects)*len(attributes))
	for s := range subjects {
		for a := range attributes {
			pairs = append(pairs, [2]int{s, a})
		}
	}
	if len(pairs) < wanted {
		return nil, fmt.Errorf("%d threads need more than the %d subject-attribute pairs available",
			wanted, len(pairs))
	}
	rng.Shuffle(len(pairs), func(i, j int) { pairs[i], pairs[j] = pairs[j], pairs[i] })

	values := allValues()
	rng.Shuffle(len(values), func(i, j int) { values[i], values[j] = values[j], values[i] })
	var used, taken int
	next := func() string {
		v := values[used]
		used++
		return v
	}
	takePair := func() (string, attribute) {
		pair := pairs[taken]
		taken++
		return subjects[pair[0]], attributes[pair[1]]
	}

	mid := yearAt(p.Years, scheduleMiddle)
	late := yearAt(p.Years, scheduleLate)
	early2 := yearAt(p.Years, scheduleEarlyPair)
	late2 := yearAt(p.Years, scheduleLatePair)

	threads := make([]Thread, 0, wanted)
	for i := 0; i < n; i++ {
		s, a := takePair()
		v := next()
		threads = append(threads, Thread{Class: classCarried, Subject: s, Attribute: a.name,
			Question: fmt.Sprintf(a.question, s),
			Asserts:  []Assert{{Year: 1, Value: v, Text: fmt.Sprintf(a.assert, s, v), Wording: wordingAssert, Replaces: true}}})
	}
	for i := 0; i < n; i++ {
		s, a := takePair()
		v1, v2, v3 := next(), next(), next()
		threads = append(threads, Thread{Class: classReplaced, Subject: s, Attribute: a.name,
			Question: fmt.Sprintf(a.question, s),
			Asserts: []Assert{
				{Year: 1, Value: v1, Text: fmt.Sprintf(a.assert, s, v1), Wording: wordingAssert, Replaces: true},
				{Year: mid, Value: v2, Text: fmt.Sprintf(a.replace, s, v2), Wording: wordingReplace, Replaces: true},
				{Year: late, Value: v3, Text: fmt.Sprintf(a.replace, s, v3), Wording: wordingReplace, Replaces: true},
			}})
	}
	for i := 0; i < n; i++ {
		s, a := takePair()
		v1, v2 := next(), next()
		threads = append(threads, Thread{Class: classReverted, Subject: s, Attribute: a.name,
			Question: fmt.Sprintf(a.question, s),
			Asserts: []Assert{
				{Year: 1, Value: v1, Text: fmt.Sprintf(a.assert, s, v1), Wording: wordingAssert, Replaces: true},
				{Year: mid, Value: v2, Text: fmt.Sprintf(a.replace, s, v2), Wording: wordingReplace, Replaces: true},
				{Year: late, Value: v1, Text: fmt.Sprintf(a.replace, s, v1), Wording: wordingReplace, Replaces: true},
			}})
	}
	for i := 0; i < n; i++ {
		s, a := takePair()
		v1, v2 := next(), next()
		threads = append(threads, Thread{Class: classParallel, Subject: s, Attribute: a.name,
			Question: fmt.Sprintf(a.question, s),
			Asserts: []Assert{
				{Year: early2, Value: v1, Text: fmt.Sprintf(a.assert, s, v1), Wording: wordingAssert, Replaces: true},
				{Year: late2, Value: v2, Text: fmt.Sprintf(a.also, s, v2), Wording: wordingAlso},
			}})
	}
	for i := 0; i < n; i++ {
		s, a := takePair()
		v1, v2 := next(), next()
		threads = append(threads, Thread{Class: classRefined, Subject: s, Attribute: a.name,
			Question: fmt.Sprintf(a.question, s),
			Asserts: []Assert{
				{Year: early2, Value: v1, Text: fmt.Sprintf(a.assert, s, v1), Wording: wordingAssert, Replaces: true},
				{Year: late2, Value: v2, Text: fmt.Sprintf(a.narrow, s, v2), Wording: wordingNarrow},
			}})
	}
	for year := 1; year <= p.Years; year++ {
		for i := 0; i < n; i++ {
			s, a := takePair()
			v := next()
			threads = append(threads, Thread{Class: classFresh, Subject: s, Attribute: a.name,
				Question: fmt.Sprintf(a.question, s),
				Asserts:  []Assert{{Year: year, Value: v, Text: fmt.Sprintf(a.assert, s, v), Wording: wordingAssert, Replaces: true}}})
		}
	}
	if used > len(values) {
		return nil, fmt.Errorf("the workload needs %d distinct values and the vocabulary holds %d",
			used, len(values))
	}
	return threads, nil
}

// yearAt maps a fraction of the span to a year, never below 2 so a
// schedule point never lands on the first year.
func yearAt(years int, fraction float64) int {
	year := int(float64(years)*fraction + 0.5)
	return max(year, 2)
}

// slot is one turn's place in the calendar.
type slot struct {
	session int
	index   int
}

// schedule places every assertion in a turn of a session in its own year,
// fills every remaining turn with noise, and returns the turns in order.
func schedule(rng *rand.Rand, p Params, threads []Thread) ([]Turn, int, error) {
	perYear := p.SessionsPerYear * p.TurnsPerSession
	free := make([][]slot, p.Years)
	for year := 1; year <= p.Years; year++ {
		slots := make([]slot, 0, perYear)
		for s := 0; s < p.SessionsPerYear; s++ {
			for i := 0; i < p.TurnsPerSession; i++ {
				slots = append(slots, slot{session: (year-1)*p.SessionsPerYear + s, index: i})
			}
		}
		rng.Shuffle(len(slots), func(i, j int) { slots[i], slots[j] = slots[j], slots[i] })
		free[year-1] = slots
	}

	days := sessionDays(rng, p)
	placed := make(map[slot]Turn)
	var factTurns int
	for i := range threads {
		thread := &threads[i]
		for j := range thread.Asserts {
			a := &thread.Asserts[j]
			slots := free[a.Year-1]
			if len(slots) == 0 {
				return nil, 0, fmt.Errorf("year %d has no free turn left for %s", a.Year, thread.Subject)
			}
			s := slots[0]
			free[a.Year-1] = slots[1:]
			a.Day = days[s.session]
			fact := Fact{
				Address: Address{Subject: thread.Subject, Attribute: thread.Attribute},
				Value:   a.Value,
			}
			declared := OperationJoin
			if a.Replaces {
				declared = OperationReplace
			}
			placed[s] = Turn{Day: a.Day, At: turnTime(a.Day, s.index), Text: a.Text,
				Fact: &fact, Declared: declared}
			factTurns++
		}
	}
	for year := 1; year <= p.Years; year++ {
		for _, s := range free[year-1] {
			placed[s] = Turn{Day: days[s.session], At: turnTime(days[s.session], s.index),
				Text: noiseText(rng)}
		}
	}

	turns := make([]Turn, 0, len(placed))
	for s, turn := range placed {
		turn.Index = s.session*p.TurnsPerSession + s.index
		turns = append(turns, turn)
	}
	sort.Slice(turns, func(i, j int) bool { return turns[i].Index < turns[j].Index })
	return turns, factTurns, nil
}

// sessionDays spreads the sessions over each year with a jitter inside the
// gap between them, so sessions stay in order and are not evenly spaced.
func sessionDays(rng *rand.Rand, p Params) []int {
	gap := daysPerYear / p.SessionsPerYear
	days := make([]int, 0, p.Years*p.SessionsPerYear)
	for year := 0; year < p.Years; year++ {
		for s := 0; s < p.SessionsPerYear; s++ {
			day := year*daysPerYear + s*gap
			if gap > 1 {
				day += rng.Intn(gap)
			}
			days = append(days, day)
		}
	}
	return days
}

// turnTime is when a turn inside a session happens. Turns are minutes
// apart, which keeps every turn of a session inside the shipped retention
// window of the turn beside it.
func turnTime(day, index int) time.Time {
	return start.AddDate(0, 0, day).Add(time.Duration(index*7) * time.Minute)
}

// Checkpoint is the turn a year's questions are asked at. It sits after the
// last session of its year and observes nothing: a frame is read at a
// checkpoint, never written to.
func Checkpoint(year int, thread Thread) Turn {
	day := year*daysPerYear - 1
	address := Address{Subject: thread.Subject, Attribute: thread.Attribute}
	return Turn{
		Day:  day,
		At:   start.AddDate(0, 0, day).Add(11 * time.Hour),
		Text: thread.Question,
		Ask:  &address,
	}
}
