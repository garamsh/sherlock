package main

import (
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/garamsh/sherlock/internal/agent"
)

// damping is the probability that the walk follows an edge rather than
// returning to its seeds: HippoRAG's setting.
const damping = 0.5

// graph is the bipartite graph of mids and the entities their syntheses
// name. Nodes 0..len(mids)-1 are mids; the rest are entities.
type graph struct {
	mids  []agent.ConversationHit
	names []string
	adj   [][]int
	// index maps a mid's ID to its node.
	index map[int64]int
	// byFirst indexes entity nodes by their first word, for seed matching.
	byFirst map[string][]int
}

// newGraph links every mid to each entity its synthesis names.
func newGraph(mids []agent.ConversationHit, entities map[int64][]string) *graph {
	g := &graph{mids: mids, adj: make([][]int, len(mids)), index: map[int64]int{}, byFirst: map[string][]int{}}
	node := map[string]int{}
	for i, m := range mids {
		g.index[m.ID] = i
		for _, name := range entities[m.ID] {
			e, ok := node[name]
			if !ok {
				e = len(g.adj)
				node[name] = e
				g.names = append(g.names, name)
				g.adj = append(g.adj, nil)
				first := strings.SplitN(name, " ", 2)[0]
				g.byFirst[first] = append(g.byFirst[first], e)
			}
			g.adj[i] = append(g.adj[i], e)
			g.adj[e] = append(g.adj[e], i)
		}
	}
	return g
}

func (g *graph) name(node int) string { return g.names[node-len(g.mids)] }

// seeds are the entity nodes whose every word occurs, contiguously, among
// the query's words. Each is weighted by its specificity, one over the
// number of mids naming it, as HippoRAG weights its seeds.
func (g *graph) seeds(query string) map[int]float64 {
	words := tokens(query)
	out := map[int]float64{}
	for i, w := range words {
		for _, e := range g.byFirst[w] {
			name := strings.Fields(g.name(e))
			if i+len(name) <= len(words) && strings.Join(words[i:i+len(name)], " ") == g.name(e) {
				out[e] = 1 / float64(len(g.adj[e]))
			}
		}
	}
	return out
}

// walk runs Personalized PageRank from the seeds and returns every node's
// score and the power iterations taken until the scores settled.
func (g *graph) walk(seeds map[int]float64) ([]float64, int) {
	restart := make([]float64, len(g.adj))
	total := 0.0
	for _, w := range seeds {
		total += w
	}
	if total == 0 {
		return restart, 0
	}
	for n, w := range seeds {
		restart[n] = w / total
	}
	score := append([]float64(nil), restart...)
	for step := 1; step <= 100; step++ {
		next := make([]float64, len(score))
		for n, s := range score {
			for _, m := range g.adj[n] {
				next[m] += damping * s / float64(len(g.adj[n]))
			}
		}
		change := 0.0
		for n := range next {
			next[n] += (1 - damping) * restart[n]
			change += math.Abs(next[n] - score[n])
		}
		score = next
		if change < 1e-12 {
			return score, step
		}
	}
	return score, 100
}

// linked is the walk's ranking of mids for a query: every mid the walk
// reached, highest score first, newer first on a tie. It also returns the
// seeds' names and the iterations taken, for the trace; for each reached
// mid the entity it was reached through: the one it names with the highest
// score, which L+A's label shows; and each reached mid's own score, which
// L+G's gate reads.
func (g *graph) linked(query string) ([]agent.ConversationHit, []string, int, map[int64]string, map[int64]float64) {
	seeds := g.seeds(query)
	score, steps := g.walk(seeds)
	var reached []int
	for i := range g.mids {
		if score[i] > 0 {
			reached = append(reached, i)
		}
	}
	sort.SliceStable(reached, func(a, b int) bool {
		if score[reached[a]] != score[reached[b]] {
			return score[reached[a]] > score[reached[b]]
		}
		return g.mids[reached[a]].ID > g.mids[reached[b]].ID
	})
	out := make([]agent.ConversationHit, len(reached))
	via, scores := map[int64]string{}, map[int64]float64{}
	for i, n := range reached {
		out[i] = g.mids[n]
		scores[g.mids[n].ID] = score[n]
		best := -1
		for _, e := range g.adj[n] {
			if best < 0 || score[e] > score[best] {
				best = e
			}
		}
		if best >= 0 {
			via[g.mids[n].ID] = g.name(best)
		}
	}
	named := []string{}
	for n := range seeds {
		named = append(named, g.name(n))
	}
	sort.Strings(named)
	return out, named, steps, via, scores
}

// namesAny reports whether a mid names one of the given entities.
func (g *graph) namesAny(mid int64, names []string) bool {
	for _, e := range g.adj[g.index[mid]] {
		if slices.Contains(names, g.name(e)) {
			return true
		}
	}
	return false
}
