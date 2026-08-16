package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bookkeeping builds a ledger with four holdings a rule can be told apart
// by: one old and read, one old and never read, one recent and read, and
// one the write called disposable.
func bookkeeping() (*ledger, []string) {
	books := newLedger()
	day := func(n int) time.Time { return start.AddDate(0, 0, n) }
	books.holdings["old-read"] = &holding{text: "old-read", written: day(0), read: day(500), salience: 0.7}
	books.holdings["old-unread"] = &holding{text: "old-unread", written: day(1), salience: 0.7}
	books.holdings["new-read"] = &holding{text: "new-read", written: day(900), read: day(950), salience: 0.7}
	books.holdings["junk"] = &holding{text: "junk", written: day(950), read: day(960), salience: 0.5, disposable: true}
	return books, []string{"old-read", "old-unread", "new-read", "junk"}
}

// Each rule takes the victim it says it takes. The rules are the round's
// candidates and every figure under a bound rests on which one is running,
// so what each picks is asserted rather than read off a table.
func TestLedgerVictims_EachRuleTakesWhatItSaysItTakes(t *testing.T) {
	books, held := bookkeeping()
	tests := []struct {
		rule  string
		first string
	}{
		{evictOldest, "old-read"},
		{evictLeastRecentlyRead, "old-unread"},
		{evictNeverRead, "old-unread"},
		{evictLowestSalience, "junk"},
		{evictDisposable, "junk"},
	}
	for _, tc := range tests {
		t.Run(tc.rule, func(t *testing.T) {
			victims := books.victims(held, map[string]bool{}, 1, tc.rule)
			require.Len(t, victims, 1)
			assert.Equal(t, tc.first, victims[0])
		})
	}
}

// Least-recently-read counts never-read as longest ago, which is what makes
// it different from oldest-first and the same as never-read at the top.
func TestLedgerVictims_CountsNeverReadAsLongestAgo(t *testing.T) {
	books, held := bookkeeping()

	assert.Equal(t, []string{"old-unread", "old-read"},
		books.victims(held, map[string]bool{}, 2, evictLeastRecentlyRead))
	assert.Equal(t, []string{"old-read", "old-unread"},
		books.victims(held, map[string]bool{}, 2, evictOldest))
}

// A rule that cannot separate two holdings still orders them the same way
// on every run, or a bounded figure would move between runs of one seed.
func TestLedgerVictims_AreTheSameOnEveryRun(t *testing.T) {
	books, held := bookkeeping()
	for _, rule := range evictionRules {
		first := books.victims(held, map[string]bool{}, 3, rule)
		for range 5 {
			assert.Equal(t, first, books.victims(held, map[string]bool{}, 3, rule), rule)
		}
	}
}

// A bound that never fires reproduces the non-result this round exists to
// correct, so what is asserted is that it fires and that the structure ends
// inside it.
func TestEnforce_BringsAFrameBackInsideItsBound(t *testing.T) {
	ctx := context.Background()
	for _, name := range frameNames {
		t.Run(name, func(t *testing.T) {
			frame, err := NewFrame(name, t.TempDir(), NewEmbedCounter(), 0)
			require.NoError(t, err)
			defer frame.Close()

			books := newLedger()
			planted := map[string]bool{}
			for i := range 40 {
				value := fmt.Sprintf("filler-%d", i)
				planted[value] = true
				turn := wrote(i, Address{Subject: subjects[i%len(subjects)], Attribute: "project"},
					value, fmt.Sprintf("%s leads the %s project.", subjects[i%len(subjects)], value))
				require.NoError(t, frame.Observe(ctx, turn))
				books.wrote(turn, planted)
				require.NoError(t, enforce(ctx, frame, books, planted, Bound{Units: 5, Rule: evictOldest}))
			}
			held, err := frame.Held(ctx)
			require.NoError(t, err)
			assert.LessOrEqual(t, len(held), 5, "%s stayed above its bound", name)
		})
	}
}

func TestEnforce_DropsNothingWithoutABound(t *testing.T) {
	ctx := context.Background()
	frame := NewAttributeFrame(aritySingle)
	books := newLedger()
	planted := map[string]bool{"amber-quarry": true}

	turn := wrote(0, almaProject, "amber-quarry", "Alma leads the amber-quarry project.")
	require.NoError(t, frame.Observe(ctx, turn))
	books.wrote(turn, planted)
	require.NoError(t, enforce(ctx, frame, books, planted, Bound{}))

	held, err := frame.Held(ctx)
	require.NoError(t, err)
	assert.Len(t, held, 1)
}

// A byte bound and a unit bound are different constraints, and which one a
// structure hits depends on what one of its units costs.
func TestEnforce_BindsOnBytesWhereAUnitBoundWouldNot(t *testing.T) {
	ctx := context.Background()
	frame := NewAttributeFrame(aritySingle)
	books := newLedger()
	planted := map[string]bool{}
	for i := range 20 {
		value := fmt.Sprintf("filler-%d", i)
		planted[value] = true
		turn := wrote(i, Address{Subject: subjects[i], Attribute: "project"}, value,
			fmt.Sprintf("%s leads the %s project.", subjects[i], value))
		require.NoError(t, frame.Observe(ctx, turn))
		books.wrote(turn, planted)
	}
	require.NoError(t, enforce(ctx, frame, books, planted, Bound{Units: 100, Bytes: 120, Rule: evictOldest}))

	held, err := frame.Held(ctx)
	require.NoError(t, err)
	var total int
	for _, text := range held {
		total += len(text)
	}
	assert.LessOrEqual(t, total, 120, "the byte bound bound it where the unit bound did not")
	assert.Less(t, len(held), 20)
}

// The read budget changes what a structure that fills one injects, and does
// nothing to a structure that returns the address it was asked for. That is
// a different shape of answer rather than a worse score, and it is why the
// budget table reports the two differently.
func TestBudget_MovesOnlyTheFramesThatFillOne(t *testing.T) {
	ctx := context.Background()
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)

	chars := func(name string, budget int) float64 {
		measured, err := MeasureUnder(ctx, w, name, armOracle, NewEmbedCounter(), nil, Under{Budget: budget})
		require.NoError(t, err)
		return measured.Years[len(measured.Years)-1].ContextChars.Mean
	}
	assert.Less(t, chars(frameDocument, 64), chars(frameDocument, 1024),
		"a bounded document is what the budget bounds")
	assert.Less(t, chars(frameShipped, 64), chars(frameShipped, 1024),
		"a ranked store fills whatever budget it is given")
	assert.Equal(t, chars(frameAttributeDeclared, 64), chars(frameAttributeDeclared, 1024),
		"an addressed store returns the address it was asked for whatever the budget")
}
