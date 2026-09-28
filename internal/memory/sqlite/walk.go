package sqlite

import (
	"context"
	"math"
	"sort"
	"strings"

	"github.com/garamsh/sherlock/internal/agent"
)

// damping is the probability that the walk follows an edge rather than
// returning to its seeds: HippoRAG's setting, as #885 studied it.
const damping = 0.5

// graph is the bipartite graph of mids and the entities their syntheses
// name. Nodes 0..len(mids)-1 are mids; the rest are entities.
type graph struct {
	mids  []agent.ConversationHit
	names []string
	adj   [][]int
	// byFirst indexes entity nodes by their first word, for seed matching.
	byFirst map[string][]int
}

// newGraph links every mid to each entity its synthesis names, numbering
// entities in the order the mids first name them.
func newGraph(mids []agent.ConversationHit, entities map[int64][]string) *graph {
	g := &graph{mids: mids, adj: make([][]int, len(mids)), byFirst: map[string][]int{}}
	node := map[string]int{}
	for i, m := range mids {
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
// the query's words. Each is weighted by one over the number of mids that
// name it, as HippoRAG weights its seeds.
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

// walk runs Personalized PageRank from the seeds until the scores change by
// less than 1e-12 in total, or for 100 iterations, and returns every node's
// score.
func (g *graph) walk(seeds map[int]float64) []float64 {
	restart := make([]float64, len(g.adj))
	total := 0.0
	for _, w := range seeds {
		total += w
	}
	if total == 0 {
		return restart
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
			break
		}
	}
	return score
}

// Linked ranks every linked mid episode the walk reaches from the stored
// entities the query names, highest score first and the newer first on a
// tie. A query naming none reaches nothing, and no model is called.
func (s *Store) Linked(ctx context.Context, query string) (agent.Linked, error) {
	mids, entities, err := s.midEntities(ctx)
	if err != nil {
		return agent.Linked{}, err
	}
	g := newGraph(mids, entities)
	seeds := g.seeds(query)
	score := g.walk(seeds)
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
	out := agent.Linked{Seeds: []string{}, Hits: make([]agent.LinkedHit, len(reached))}
	for n := range seeds {
		out.Seeds = append(out.Seeds, g.name(n))
	}
	sort.Strings(out.Seeds)
	for i, n := range reached {
		out.Hits[i] = agent.LinkedHit{ConversationHit: g.mids[n], Subject: entities[g.mids[n].ID][0]}
	}
	return out, nil
}
