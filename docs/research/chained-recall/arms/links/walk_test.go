package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
)

// chainGraph links mids 1 to 3 as a chain through entities b and c; mid 4
// shares a common entity with mid 1; mid 5 is unconnected.
func chainGraph() *graph {
	mids := []agent.ConversationHit{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}, {ID: 5}}
	return newGraph(mids, map[int64][]string{
		1: {"rodend", "gebind components", "office"},
		2: {"gebind components", "mateo deatreal"},
		3: {"mateo deatreal"},
		4: {"office"},
		5: {"harbour"},
	})
}

func ids(hits []agent.ConversationHit) []int64 {
	out := []int64{}
	for _, h := range hits {
		out = append(out, h.ID)
	}
	return out
}

func TestSeeds_MatchesWholeNamesInTheQueryWeightedBySpecificity(t *testing.T) {
	g := chainGraph()
	_, seeds, _ := g.linked("Who handles Gebind Components for the Rodend build?")
	assert.Equal(t, []string{"gebind components", "rodend"}, seeds)
	_, seeds, _ = g.linked("Who handles Gebind for the build?")
	assert.Empty(t, seeds, "the control: part of a name is no seed")
	weights := g.seeds("rodend office")
	require.Len(t, weights, 2)
	for n, w := range weights {
		assert.InDelta(t, 1/float64(len(g.adj[n])), w, 1e-12, g.name(n))
	}
}

func TestLinked_FollowsTheChainPastTheSeeds(t *testing.T) {
	g := chainGraph()
	hits, seeds, steps := g.linked("What number do I dial about the Rodend build?")
	assert.Equal(t, []string{"rodend"}, seeds)
	assert.Greater(t, steps, 1)
	assert.Equal(t, []int64{1, 2, 4, 3}, ids(hits), "the seed's mid first, then one entity away, then two")
	assert.False(t, g.namesAny(3, seeds), "mid 3 names no seed: the walk crossed two entities to reach it")
	none, _, noSteps := g.linked("What number do I dial?")
	assert.Empty(t, none, "the control: a query naming no stored entity reaches nothing")
	assert.Zero(t, noSteps)
}

func TestWalk_KeepsTheScoresADistribution(t *testing.T) {
	g := chainGraph()
	score, _ := g.walk(g.seeds("rodend"))
	total := 0.0
	for _, s := range score {
		total += s
	}
	assert.InDelta(t, 1, total, 1e-9)
	assert.Zero(t, score[4], "mid 5 is not connected to the seed")
}
