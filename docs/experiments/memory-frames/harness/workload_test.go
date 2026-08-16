package main

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// smallParams is a workload of the same shape as the measured one and a
// fraction of its size, so a test exercises every class without paying for
// a decade of turns.
func smallParams() Params {
	return Params{Years: 10, SessionsPerYear: 4, TurnsPerSession: 8, ThreadsPerClass: 3}
}

func TestGenerateWorkload_IsDeterministicInTheSeed(t *testing.T) {
	first, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)
	second, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)
	other, err := GenerateWorkload(8, smallParams())
	require.NoError(t, err)

	assert.Equal(t, first.TurnDigest, second.TurnDigest, "the same seed must produce the same turns")
	assert.Equal(t, first.Threads, second.Threads)
	assert.NotEqual(t, first.TurnDigest, other.TurnDigest, "a different seed must produce different turns")
}

func TestGenerateWorkload_ReportsTheCountsItPlanted(t *testing.T) {
	p := smallParams()
	w, err := GenerateWorkload(7, p)
	require.NoError(t, err)

	assert.Equal(t, p.Years*p.SessionsPerYear*p.TurnsPerSession, w.Params.Turns)
	assert.Equal(t, len(w.Turns), w.Params.Turns)
	assert.Equal(t, w.Params.Turns-w.Params.FactTurns, w.Params.NoiseTurns)

	var facts int
	for _, turn := range w.Turns {
		facts += len(turn.Written)
	}
	assert.Equal(t, facts, w.Params.FactTurns)

	var assertions int
	for _, thread := range w.Threads {
		assertions += len(thread.Asserts)
	}
	assert.Equal(t, assertions, w.Params.FactTurns, "every assertion must reach exactly one turn")
	assert.Greater(t, w.Params.NoiseTurns, w.Params.FactTurns,
		"most turns must carry no fact, or the workload is not a conversation")
}

func TestGenerateWorkload_PlantsEveryClassAtTheYearsItPromises(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)

	years := map[string][]int{}
	for _, thread := range w.Threads {
		if thread.Class == classFresh {
			continue
		}
		var seen []int
		for _, a := range thread.Asserts {
			seen = append(seen, a.Year)
		}
		if _, ok := years[thread.Class]; !ok {
			years[thread.Class] = seen
		}
		assert.Equal(t, years[thread.Class], seen, "%s threads must share one schedule", thread.Class)
	}
	assert.Equal(t, []int{1}, years[classCarried])
	assert.Equal(t, []int{1, 3, 7}, years[classReplaced])
	assert.Equal(t, []int{1, 3, 7}, years[classReverted])
	assert.Equal(t, []int{2, 6}, years[classParallel])
	assert.Equal(t, []int{2, 6}, years[classRefined])
}

func TestGenerateWorkload_PlantsEveryValueOnce(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)

	planted := map[string][]string{}
	for _, thread := range w.Threads {
		for _, a := range thread.Asserts {
			planted[a.Value] = append(planted[a.Value], thread.Subject+"/"+thread.Attribute)
		}
	}
	for value, owners := range planted {
		unique := map[string]bool{}
		for _, owner := range owners {
			unique[owner] = true
		}
		assert.Len(t, unique, 1,
			"value %q is planted on more than one thread, so crediting it would be ambiguous", value)
	}
}

// A turn that carries no fact must carry no planted value either. Without
// this the noise would credit answers it was never meant to hold, and every
// recall figure would be measuring the workload's own filler.
func TestGenerateWorkload_NoNoiseTurnCarriesAPlantedValue(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)

	var values []string
	for _, thread := range w.Threads {
		for _, a := range thread.Asserts {
			values = append(values, a.Value)
		}
	}
	for _, turn := range w.Turns {
		if len(turn.Written) > 0 {
			continue
		}
		for _, value := range values {
			require.False(t, Mentions(turn.Text, value),
				"a turn carrying no fact mentions the planted value %q: %q", value, turn.Text)
		}
	}
}

// A fact-carrying turn says exactly the value it was planted with, so a
// context holding that turn's text is credited for that fact and no other.
func TestGenerateWorkload_EveryFactTurnSaysItsOwnValue(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)

	for _, turn := range w.Turns {
		for _, written := range turn.Written {
			assert.True(t, Mentions(turn.Text, written.Value),
				"turn %q does not say its own value %q", turn.Text, written.Value)
			assert.True(t, strings.Contains(turn.Text, written.Subject),
				"turn %q does not name its own subject %q", turn.Text, written.Subject)
		}
	}
}

func TestGenerateWorkload_TurnsRunForwardInTime(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)

	for i := 1; i < len(w.Turns); i++ {
		assert.Greater(t, w.Turns[i].Index, w.Turns[i-1].Index)
		assert.GreaterOrEqual(t, w.Turns[i].Day, w.Turns[i-1].Day)
		assert.False(t, w.Turns[i].At.Before(w.Turns[i-1].At))
	}
	last := w.Turns[len(w.Turns)-1]
	assert.Less(t, last.Day, w.Params.Years*daysPerYear)
}

func TestGenerateWorkload_PlacesEveryAssertionInsideItsOwnYear(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)

	// A reverted thread asserts one value twice, so a value maps to the
	// years it was said in rather than to one year.
	planted := map[string][]int{}
	for _, turn := range w.Turns {
		for _, written := range turn.Written {
			planted[written.Value] = append(planted[written.Value], turn.Day/daysPerYear+1)
		}
	}
	expected := map[string][]int{}
	for _, thread := range w.Threads {
		for _, a := range thread.Asserts {
			expected[a.Value] = append(expected[a.Value], a.Year)
		}
	}
	for value, years := range expected {
		sort.Ints(years)
		got := planted[value]
		sort.Ints(got)
		assert.Equal(t, years, got, "the turns saying %q fall in the wrong years", value)
	}
}

// The day stamped on an assertion is what §What the addresses look like
// reads the interval between two writes from, so an assertion carrying a
// day it was not said on is a wrong figure in a published table.
func TestGenerateWorkload_StampsEachAssertionWithTheDayItWasSaid(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)

	// A reverted thread asserts one value twice, so an address and a value
	// map to the days they were said on rather than to one day.
	said := map[Fact][]int{}
	for _, turn := range w.Turns {
		for _, written := range turn.Written {
			said[written.Fact] = append(said[written.Fact], turn.Day)
		}
	}
	var repeated int
	for _, thread := range w.Threads {
		address := Address{Subject: thread.Subject, Attribute: thread.Attribute}
		stamped := map[Fact][]int{}
		for _, a := range thread.Asserts {
			assert.Equal(t, a.Year, a.Day/daysPerYear+1,
				"%q is stamped with a day outside the year it was asserted in", a.Value)
			fact := Fact{Address: address, Value: a.Value}
			stamped[fact] = append(stamped[fact], a.Day)
		}
		for fact, days := range stamped {
			if len(days) > 1 {
				repeated++
			}
			sort.Ints(days)
			got := append([]int(nil), said[fact]...)
			sort.Ints(got)
			assert.Equal(t, got, days, "the days stamped on %q are not the days it was said on", fact.Value)
		}
	}
	require.NotZero(t, repeated,
		"the workload must assert one value twice, or this checks nothing a single assertion would not")
}

func TestGenerateWorkload_RejectsASpanTooShortForItsSchedule(t *testing.T) {
	p := smallParams()
	p.Years = 3
	_, err := GenerateWorkload(7, p)
	assert.Error(t, err)
}

func TestGenerateWorkload_RejectsMoreThreadsThanTheVocabularyHolds(t *testing.T) {
	p := smallParams()
	p.ThreadsPerClass = len(subjects) * len(attributes)
	_, err := GenerateWorkload(7, p)
	assert.Error(t, err)
}

// TruthAt is the workload's ground truth, and every recall and stale figure
// is read off it. The six classes differ only here.
func TestThreadTruthAt_TracksEachClassThroughTheDecade(t *testing.T) {
	carried := Thread{Class: classCarried, Asserts: []Assert{
		{Year: 1, Value: "a", Replaces: true}}}
	replaced := Thread{Class: classReplaced, Asserts: []Assert{
		{Year: 1, Value: "a", Replaces: true},
		{Year: 3, Value: "b", Replaces: true},
		{Year: 7, Value: "c", Replaces: true}}}
	reverted := Thread{Class: classReverted, Asserts: []Assert{
		{Year: 1, Value: "a", Replaces: true},
		{Year: 3, Value: "b", Replaces: true},
		{Year: 7, Value: "a", Replaces: true}}}
	parallel := Thread{Class: classParallel, Asserts: []Assert{
		{Year: 2, Value: "a", Replaces: true},
		{Year: 6, Value: "b"}}}

	tests := []struct {
		name       string
		thread     Thread
		year       int
		live       []string
		superseded []string
	}{
		{"a carried fact is still true nine years later", carried, 10, []string{"a"}, nil},
		{"a carried fact has nothing to supersede", carried, 1, []string{"a"}, nil},
		{"a replaced fact holds its first value until the change", replaced, 2, []string{"a"}, nil},
		{"a replaced fact holds its second value after it", replaced, 3, []string{"b"}, []string{"a"}},
		{"a replaced fact holds only its last value", replaced, 10, []string{"c"}, []string{"a", "b"}},
		{"a reverted fact holds the replacement in between", reverted, 6, []string{"b"}, []string{"a"}},
		{"a value returned to is live and not also stale", reverted, 7, []string{"a"}, []string{"b"}},
		{"parallel values are both true and neither supersedes", parallel, 6, []string{"a", "b"}, nil},
		{"a parallel thread holds one value before the second", parallel, 5, []string{"a"}, nil},
		{"a thread has no truth before its first assertion", parallel, 1, nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			live, superseded := tc.thread.TruthAt(tc.year)
			assert.Equal(t, tc.live, live)
			assert.Equal(t, tc.superseded, superseded)
		})
	}
}

func TestThreadProbedAt_AsksAFreshThreadOnlyInItsOwnYear(t *testing.T) {
	fresh := Thread{Class: classFresh, Asserts: []Assert{{Year: 4, Value: "a", Replaces: true}}}
	carried := Thread{Class: classCarried, Asserts: []Assert{{Year: 4, Value: "a", Replaces: true}}}

	assert.False(t, fresh.ProbedAt(3))
	assert.True(t, fresh.ProbedAt(4))
	assert.False(t, fresh.ProbedAt(5))
	assert.False(t, carried.ProbedAt(3))
	assert.True(t, carried.ProbedAt(4))
	assert.True(t, carried.ProbedAt(10))
}
