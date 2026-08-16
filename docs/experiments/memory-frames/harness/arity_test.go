package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// evidenceParams is a workload wide enough that every attribute carries
// threads from every class, which is what the two claims below are about.
func evidenceParams() Params {
	return Params{Years: 10, SessionsPerYear: 16, TurnsPerSession: 16, ThreadsPerClass: 20}
}

// The evidence table has to describe the addresses a frame actually sees,
// so it is read off the same threads the turns were built from.
func TestMeasureArity_CountsOneAddressPerThread(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)

	arity := MeasureArity(w)

	var byClass, byAttribute int
	for _, c := range arity.Classes {
		byClass += c.Addresses
	}
	for _, a := range arity.Attributes {
		byAttribute += a.Addresses
	}
	assert.Equal(t, len(w.Threads), byClass)
	assert.Equal(t, len(w.Threads), byAttribute)
	assert.Len(t, arity.Classes, len(classNames))
	assert.Len(t, arity.Attributes, len(attributes))
}

// A class whose addresses are written once has no interval to report, and a
// figure with no sample says so rather than reporting zero.
func TestMeasureArity_ReportsNoIntervalForAnAddressWrittenOnce(t *testing.T) {
	w, err := GenerateWorkload(7, smallParams())
	require.NoError(t, err)

	byClass := map[string]ClassAddresses{}
	for _, c := range MeasureArity(w).Classes {
		byClass[c.Class] = c
	}
	assert.Equal(t, 0, byClass[classCarried].Gap.N)
	assert.Equal(t, 0, byClass[classFresh].Gap.N)
	assert.Equal(t, 1.0, byClass[classCarried].Values.Mean)
	assert.Positive(t, byClass[classReplaced].Gap.N)
}

// The interval between the last two writes is what a timing rule would
// read. A class that must be replaced and a class that must coexist sharing
// that interval is why no such rule can separate them here, and it is
// measured rather than argued.
func TestMeasureArity_GivesReplacementAndCoexistenceTheSameInterval(t *testing.T) {
	w, err := GenerateWorkload(7, evidenceParams())
	require.NoError(t, err)

	byClass := map[string]ClassAddresses{}
	for _, c := range MeasureArity(w).Classes {
		byClass[c.Class] = c
	}
	replaced, parallel := byClass[classReplaced].Gap, byClass[classParallel].Gap
	assert.InDelta(t, replaced.Mean, parallel.Mean, replaced.Half+parallel.Half,
		"the intervals overlap, so no window separates the two")
}

// A rule that decides arity per attribute reads the share column. Every
// attribute carrying threads from every class puts every row on the same
// side of any threshold, which is the measured reason such a rule collapses.
func TestMeasureArity_PutsEveryAttributeOnTheSameSide(t *testing.T) {
	w, err := GenerateWorkload(7, evidenceParams())
	require.NoError(t, err)

	arity := MeasureArity(w)
	require.NotEmpty(t, arity.Attributes)
	for _, a := range arity.Attributes {
		assert.Positive(t, a.MultiValued, "%s has no address written twice", a.Attribute)
		assert.Less(t, a.Share, 0.5, "%s has a majority of addresses written twice", a.Attribute)
	}
}

func TestDistinctValues_CountsAValueWrittenTwiceOnce(t *testing.T) {
	reverted := Thread{Asserts: []Assert{
		{Value: "a"}, {Value: "b"}, {Value: "a"},
	}}
	assert.Equal(t, 2, distinctValues(reverted))
	assert.Equal(t, 1, distinctValues(Thread{Asserts: []Assert{{Value: "a"}}}))
}
