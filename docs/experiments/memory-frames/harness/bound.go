package main

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/garamsh/gagent/internal/gateway"
)

// An unbounded store makes every structure look manageable, because nothing
// ever has to be given up. Every figure before this round was measured
// where nothing had to be discarded and nothing had to be left out of the
// context, and the programme's question — which structure holds and updates
// memory over years — only becomes a comparison under a constraint.
//
// What decides an eviction is treated the way arity was treated: candidate
// rules, each measured, none tuned, and a rule needing a threshold
// disqualified rather than fitted. The rules live here rather than inside
// any frame, so no structure carries a clause for one and every structure
// is bound the same way.

// The candidate rules. The first four decide from what the store holds or
// from what has happened to it; the last is told by the write, which is
// where this record concluded arity belongs and is therefore the one to
// watch.
const (
	// evictOldest drops what was written longest ago.
	evictOldest = "oldest"
	// evictLeastRecentlyRead drops what has gone longest without reaching a
	// context, counting never as longest.
	evictLeastRecentlyRead = "least-recently-read"
	// evictNeverRead drops what no context has ever carried, and falls back
	// to the oldest when everything has been read. It is the rule #468
	// defined and could not fire.
	evictNeverRead = "never-read"
	// evictLowestSalience drops the least salient first, which is what the
	// shipped design's own eviction does with its lifecycle score.
	evictLowestSalience = "lowest-salience"
	// evictDisposable drops what the write marked as carrying no fact,
	// before anything else. Nothing about the store decides it.
	evictDisposable = "declared-disposable"
)

// evictionRules is the order every table reports the rules in.
var evictionRules = []string{
	evictOldest, evictLeastRecentlyRead, evictNeverRead, evictLowestSalience, evictDisposable,
}

// Bound is a capacity a structure must stay inside and the rule that
// decides what goes when it does not. A zero Bound constrains nothing,
// which is what every earlier round ran under.
type Bound struct {
	Units int    `json:"units"`
	Bytes int64  `json:"bytes"`
	Rule  string `json:"rule"`
}

// binds reports whether this bound constrains anything at all.
func (b Bound) binds() bool { return b.Units > 0 || b.Bytes > 0 }

// Under is what a run constrains a frame with: a capacity, and the read
// budget it answers within. The zero value is the unconstrained run every
// earlier figure was measured at.
type Under struct {
	Bound  Bound `json:"bound"`
	Budget int   `json:"budget"`
}

// holding is what the run knows about one thing a structure holds. The
// frame is never asked for any of it: the run sees every write and every
// context, so it can keep this itself, which is what lets the rules sit
// outside the structures they evict from.
type holding struct {
	text string
	// written and read are when the run first handed this text to the
	// frame and when a context last carried it. read is the zero time while
	// no context has.
	written time.Time
	read    time.Time
	// salience is what the write would have carried into the shipped
	// store's ranking, derived from the same op kind, so the rule that
	// stands in for what that design does today reads a real value.
	salience float64
	// disposable is what the write says: a turn carrying no extracted fact
	// is disposable, and nothing about the store could have known it.
	disposable bool
}

// ledger is the run's record of what it has handed a frame and what has
// come back out of one.
type ledger struct {
	holdings map[string]*holding
	order    []string
}

func newLedger() *ledger { return &ledger{holdings: map[string]*holding{}} }

// wrote records a turn as it was handed to the frame. The text a structure
// ends up holding for it is not necessarily this text, which is why the
// ledger is consulted by value below and tolerates a miss.
func (l *ledger) wrote(t Turn, planted map[string]bool) {
	disposable := len(t.Written) == 0
	salience := gatewaySalienceAdd(len(t.Written) > 0)
	for _, value := range valuesIn(t.Text, planted) {
		l.note(value, t.At, salience, disposable)
	}
	if len(t.Written) > 0 {
		for _, written := range t.Written {
			l.note(written.Value, t.At, salience, disposable)
		}
		return
	}
	l.note(t.Text, t.At, salience, disposable)
}

func (l *ledger) note(key string, at time.Time, salience float64, disposable bool) {
	if _, seen := l.holdings[key]; seen {
		return
	}
	l.holdings[key] = &holding{text: key, written: at, salience: salience, disposable: disposable}
	l.order = append(l.order, key)
}

// read records that a context carried these values.
func (l *ledger) read(values []string, at time.Time) {
	for _, value := range values {
		if one, ok := l.holdings[value]; ok {
			one.read = at
		}
	}
}

// victims chooses what a frame gives up to come back inside the bound. It
// returns the held texts to drop, in the order the rule ranks them.
func (l *ledger) victims(held []string, planted map[string]bool, over int, rule string) []string {
	if over <= 0 {
		return nil
	}
	ranked := make([]string, len(held))
	copy(ranked, held)
	weight := func(text string) *holding {
		for _, value := range valuesIn(text, planted) {
			if one, ok := l.holdings[value]; ok {
				return one
			}
		}
		if one, ok := l.holdings[text]; ok {
			return one
		}
		return &holding{text: text, disposable: true}
	}
	// The comparison falls back to the text so that two holdings a rule
	// cannot separate are still ordered the same way on every run.
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := weight(ranked[i]), weight(ranked[j])
		switch rule {
		case evictOldest:
			if !a.written.Equal(b.written) {
				return a.written.Before(b.written)
			}
		case evictLeastRecentlyRead:
			if !a.read.Equal(b.read) {
				return a.read.Before(b.read)
			}
		case evictNeverRead:
			if a.read.IsZero() != b.read.IsZero() {
				return a.read.IsZero()
			}
			if !a.written.Equal(b.written) {
				return a.written.Before(b.written)
			}
		case evictLowestSalience:
			if a.salience != b.salience {
				return a.salience < b.salience
			}
			if !a.written.Equal(b.written) {
				return a.written.Before(b.written)
			}
		case evictDisposable:
			if a.disposable != b.disposable {
				return a.disposable
			}
			if !a.written.Equal(b.written) {
				return a.written.Before(b.written)
			}
		}
		return ranked[i] < ranked[j]
	})
	if over > len(ranked) {
		over = len(ranked)
	}
	return ranked[:over]
}

// enforce brings a frame back inside the bound, dropping whatever the rule
// ranks first. A bound on bytes drops until the held text fits; a bound on
// units drops until the count does.
func enforce(ctx context.Context, frame Frame, l *ledger, planted map[string]bool, b Bound) error {
	if !b.binds() {
		return nil
	}
	held, err := frame.Held(ctx)
	if err != nil {
		return fmt.Errorf("held: %w", err)
	}
	over := 0
	if b.Units > 0 && len(held) > b.Units {
		over = len(held) - b.Units
	}
	if b.Bytes > 0 {
		var total int64
		sizes := make([]int64, len(held))
		for i, text := range held {
			sizes[i] = int64(len(text))
			total += sizes[i]
		}
		// Dropping in the rule's order, count how many it takes to fit.
		if total > b.Bytes {
			ranked := l.victims(held, planted, len(held), b.Rule)
			byText := map[string]int64{}
			for i, text := range held {
				byText[text] = sizes[i]
			}
			needed := 0
			for _, text := range ranked {
				if total <= b.Bytes {
					break
				}
				total -= byText[text]
				needed++
			}
			if needed > over {
				over = needed
			}
		}
	}
	if over <= 0 {
		return nil
	}
	if err := frame.Forget(ctx, l.victims(held, planted, over, b.Rule)); err != nil {
		return fmt.Errorf("forget: %w", err)
	}
	return nil
}

// gatewaySalienceAdd is the salience the shipped writeback derives for an
// add, read from internal/gateway rather than restated, so the rule that
// stands in for that design reads the value that design would.
func gatewaySalienceAdd(carriesFact bool) float64 {
	if carriesFact {
		return gateway.SalienceAddSemantic
	}
	return gateway.SalienceAddEpisodic
}

// The constraints swept. The capacities are chosen to bite: the loosest sits
// just under what the largest structure ends the decade holding, and the
// tightest is a small fraction of it, so eviction fires from barely to
// severely rather than not at all.
var (
	sweptBounds  = []int{800, 200, 50}
	sweptBudgets = []int{64, 128, 256, 512, 1024, 2048}
)

// The three comparisons a constrained run belongs to.
const (
	sweepCapacity = "capacity"
	sweepEviction = "eviction"
	sweepBudget   = "budget"
)

// ruleBound is the capacity the eviction rules are compared at. One hard
// bound rather than a grid: a rule that cannot be told apart from another
// where nothing has to go is not being measured.
const ruleBound = 200

// sweepRule is the rule the capacity sweep runs under. Oldest-first carries
// no information about use or content, so what the other rules buy is read
// against it rather than against nothing.
const sweepRule = evictOldest

// Constrained is one frame measured inside one constraint. Bound and Budget
// say which constraint, and a zero in either says that one was not applied.
type Constrained struct {
	// Sweep names which comparison this row belongs to, so a run that
	// appears in two of them is reported in one.
	Sweep  string       `json:"sweep"`
	Frame  string       `json:"frame"`
	Bound  Bound        `json:"bound"`
	Budget int          `json:"budget"`
	Years  []YearResult `json:"years"`
	Reach  []YearReach  `json:"reach"`
}

// MeasureConstrained runs every frame under the capacity sweep, the rule
// comparison and the read-budget sweep.
//
// The oracle arm alone: the constraint question is what a structure does
// when it must give something up, and in the text arm the addressed
// structures hold nothing to give.
func MeasureConstrained(ctx context.Context, w Workload) ([]Constrained, error) {
	var out []Constrained
	run := func(sweep, frame string, under Under) error {
		measured, err := MeasureUnder(ctx, w, frame, armOracle, NewEmbedCounter(), nil, under)
		if err != nil {
			return fmt.Errorf("%s under %+v: %w", frame, under, err)
		}
		out = append(out, Constrained{Sweep: sweep, Frame: frame, Bound: under.Bound,
			Budget: under.Budget, Years: measured.Years, Reach: measured.Reach})
		return nil
	}
	for _, units := range sweptBounds {
		for _, frame := range frameNames {
			if err := run(sweepCapacity, frame, Under{Bound: Bound{Units: units, Rule: sweepRule}}); err != nil {
				return nil, err
			}
		}
	}
	for _, rule := range evictionRules {
		for _, frame := range frameNames {
			if err := run(sweepEviction, frame, Under{Bound: Bound{Units: ruleBound, Rule: rule}}); err != nil {
				return nil, err
			}
		}
	}
	for _, budget := range sweptBudgets {
		for _, frame := range frameNames {
			if err := run(sweepBudget, frame, Under{Budget: budget}); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}
