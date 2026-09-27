package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var passIntervals = map[int64][2]int64{10: {37, 40}, 11: {41, 44}}

func seeded(t *testing.T, n int) Document {
	t.Helper()
	var d Document
	var ops []Op
	for i := range n {
		ops = append(ops, Op{Op: "add", Text: "fact " + strings.Repeat("x", i), Mids: []int64{10}})
	}
	_, err := d.Apply(ops, 1, passIntervals, true)
	require.NoError(t, err)
	require.Len(t, d.Lines, n)
	return d
}

func TestApply_AddsSupersedesAndDropsAndKeepsEveryVersion(t *testing.T) {
	d := seeded(t, 4)
	a, err := d.Apply([]Op{
		{Op: "supersede", Line: 2, Text: "fact x corrected", Mids: []int64{11}},
		{Op: "drop", Line: 1},
		{Op: "add", Text: "a new fact", Mids: []int64{10, 11}},
	}, 2, passIntervals, true)
	require.NoError(t, err)
	assert.Equal(t, Applied{Added: 1, Superseded: 1, Dropped: 1}, a)
	require.Len(t, d.Lines, 4)
	assert.Equal(t, "fact x corrected", d.Lines[0].Text)
	assert.Equal(t, int64(41), d.Lines[0].First)
	assert.Equal(t, [2]int64{37, 44}, [2]int64{d.Lines[3].First, d.Lines[3].Last}, "a line spans the intervals of the mids it cites")
	assert.Len(t, d.History, 7, "four adds, then a supersede, a drop and an add")
	assert.Equal(t, "fact", d.History[0].Text, "the dropped line's first version is kept")
	assert.True(t, strings.HasPrefix(d.Render(), "1. fact x corrected (mids 11)\n"))
}

func TestApply_RejectsAnOperationThatBreaksARule(t *testing.T) {
	d := seeded(t, 2)
	a, err := d.Apply([]Op{
		{Op: "add", Text: "cites a mid the pass was not given", Mids: []int64{99}},
		{Op: "add", Text: "cites nothing"},
		{Op: "add", Text: strings.Repeat("y", lineRunes+1), Mids: []int64{10}},
		{Op: "supersede", Line: 3, Text: "no such line", Mids: []int64{10}},
		{Op: "rename", Line: 1},
		{Op: "add", Text: "a valid line", Mids: []int64{10}},
	}, 2, passIntervals, true)
	require.NoError(t, err)
	assert.Equal(t, Applied{Added: 1, Rejected: 5}, a, "the control: the valid operation is applied")
	assert.Len(t, d.Lines, 3)
}

func TestApply_RefusesMoreThanAQuarterOfTheLinesDroppedWhenStrict(t *testing.T) {
	d := seeded(t, 8)
	_, err := d.Apply([]Op{{Op: "drop", Line: 1}, {Op: "drop", Line: 2}, {Op: "drop", Line: 3}}, 2, passIntervals, true)
	require.True(t, errors.Is(err, ErrRefused))
	assert.Len(t, d.Lines, 8, "a refused pass changes nothing")

	a, err := d.Apply([]Op{{Op: "drop", Line: 1}, {Op: "drop", Line: 2}, {Op: "drop", Line: 3}}, 2, passIntervals, false)
	require.NoError(t, err)
	assert.Equal(t, Applied{Dropped: 2, Skipped: 1}, a, "not strict, drops past the quarter are skipped")
	_, err = d.Apply([]Op{{Op: "drop", Line: 1}}, 3, passIntervals, true)
	assert.NoError(t, err, "the control: a drop within the quarter is applied")
}

func TestApply_HoldsTheBound(t *testing.T) {
	var d Document
	var ops []Op
	for range 16 {
		ops = append(ops, Op{Op: "add", Text: strings.Repeat("z", lineRunes), Mids: []int64{10}})
	}
	for d.Chars()+16*(lineRunes+20) <= Bound {
		_, err := d.Apply(ops, 1, passIntervals, true)
		require.NoError(t, err)
	}
	before := len(d.Lines)
	_, err := d.Apply(ops, 2, passIntervals, true)
	require.True(t, errors.Is(err, ErrRefused), "strict, a pass past the bound is refused")
	assert.Len(t, d.Lines, before)

	a, err := d.Apply(ops, 2, passIntervals, false)
	require.NoError(t, err)
	assert.Positive(t, a.Skipped)
	assert.LessOrEqual(t, d.Chars(), Bound)
	assert.Greater(t, len(d.Lines), before, "the control: adds that fit are applied")
}

func TestLineItems_LabelEachLineWithTheMidsItCites(t *testing.T) {
	d := seeded(t, 1)
	items := LineItems(d)
	require.Len(t, items, 1)
	assert.Equal(t, "line", items[0].Level)
	assert.Equal(t, "[line:0 mids:10; model-written]", items[0].Label)
	assert.Equal(t, int64(37), items[0].SourceFirst)
}

func TestApply_PartialPassSkipsSupersedesThatWouldPassTheBound(t *testing.T) {
	var d Document
	short := []Op{}
	for range 16 {
		short = append(short, Op{Op: "add", Text: strings.Repeat("s", 150), Mids: []int64{10}})
	}
	for d.Chars()+16*170 <= Bound {
		_, err := d.Apply(short, 1, passIntervals, true)
		require.NoError(t, err)
	}
	lines := len(d.Lines)
	grow := []Op{}
	for i := 1; i <= 16 && i <= lines; i++ {
		grow = append(grow, Op{Op: "supersede", Line: i, Text: strings.Repeat("g", lineRunes), Mids: []int64{11}})
	}
	grow = append(grow, Op{Op: "supersede", Line: lines, Text: "shorter", Mids: []int64{11}})
	a, err := d.Apply(grow, 2, passIntervals, false)
	require.NoError(t, err)
	assert.LessOrEqual(t, d.Chars(), Bound)
	assert.Equal(t, 1, a.Superseded, "the control: a supersede that shortens its line is applied")
	assert.Equal(t, "shorter", d.Lines[lines-1].Text)
}
