package main

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"

	"github.com/garamsh/sherlock/docs/research/chained-recall/study"
	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

// The shipped recall's budget: the two newest mids, then five historical
// slots (internal/agent/memory/conversation.go).
const (
	recentMids      = 2
	historicalSlots = 5
)

// memory is an opened L store: the production store under it, answering
// recency and similarity, and the entity graph, answering the walk.
type memory struct {
	store *sqlite.Store
	graph *graph
	trace map[string]any
	// associate is L+A's label: a walk-delivered mid is delivered as
	// "associated", its text opened by the entity it was reached through.
	associate bool
	// gated is L+G's gate (#888): a walk mid that names none of the
	// query's own entities is delivered only when its walk score reaches
	// gateScore.
	gated bool
}

// gateScore is L+G's threshold on a walk mid's Personalized PageRank
// score, fixed by readtime/preregistration.md from #885's offline records
// (readtime/evidence/calibration/gate.json). No finite threshold kept the
// associated episodes of at most 3 of #885's 60 absent queries while
// keeping a later hop, so the gate passes only walk mids that name one of
// the query's own entities.
var gateScore = math.Inf(1)

// gate keeps the walk mids that name one of the query's own entities, and
// the others whose walk score reaches gateScore, in the walk's order.
func (m *memory) gate(linked []agent.ConversationHit, seeds []string, scores map[int64]float64) []agent.ConversationHit {
	var out []agent.ConversationHit
	for _, h := range linked {
		if m.graph.namesAny(h.ID, seeds) || scores[h.ID] >= gateScore {
			out = append(out, h)
		}
	}
	return out
}

// associatedLabel opens the text of a mid L+A's walk delivered, so the
// model reads it as reached through an entity rather than matched to the
// question.
func associatedLabel(entity string) string {
	return fmt.Sprintf("Associated through the named entity %q, not matched to the question: ", entity)
}

// Recall returns the two newest mids, then fills the shipped recall's five
// historical slots from the similar list and the walk's list in turn, so
// the walk's mids take similar ones' places rather than adding context.
func (m *memory) Recall(ctx context.Context, task string) ([]study.Item, error) {
	recent, err := m.store.RecentMid(ctx, recentMids)
	if err != nil {
		return nil, err
	}
	similar, err := m.store.Search(ctx, task, historicalSlots+len(recent))
	if err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	items := take(recentMids, seen, &list{"recent", recent})
	return append(items, m.alternate(task, historicalSlots, seen, similar)...), nil
}

// Search answers memory_search the same way: limit slots, taken in turn
// from the similar list and the walk's list.
func (m *memory) Search(ctx context.Context, query string, limit int) ([]study.Item, error) {
	similar, err := m.store.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	return m.alternate(query, limit, map[int64]bool{}, similar), nil
}

// alternate fills slots from the similar list and the walk's list in turn
// and records the walk's trace: the seeds, the iterations, the mids it
// reached, and which delivered mids came from it, lay outside the similar
// list, and named no seed.
func (m *memory) alternate(query string, slots int, seen map[int64]bool, similar []agent.ConversationHit) []study.Item {
	linked, seeds, steps, via, scores := m.graph.linked(query)
	var ungated []study.Item
	if m.gated {
		ungated = take(slots, maps.Clone(seen), &list{"historical", similar}, &list{"linked", linked})
		linked = m.gate(linked, seeds, scores)
	}
	items := take(slots, seen, &list{"historical", similar}, &list{"linked", linked})
	if m.associate {
		for i, it := range items {
			if it.Kind == "linked" {
				items[i].Kind = "associated"
				items[i].Text = associatedLabel(via[it.ID]) + it.Text
			}
		}
	}
	inSimilar := map[int64]bool{}
	for _, h := range similar {
		inSimilar[h.ID] = true
	}
	delivered, beyond, far := []int64{}, []int64{}, []int64{}
	for _, it := range items {
		if it.Kind != "linked" && it.Kind != "associated" {
			continue
		}
		delivered = append(delivered, it.ID)
		if !inSimilar[it.ID] {
			beyond = append(beyond, it.ID)
		}
		if !m.graph.namesAny(it.ID, seeds) {
			far = append(far, it.ID)
		}
	}
	m.trace = map[string]any{"seeds": seeds, "steps": steps, "reached": len(linked), "linked": delivered, "beyond_similar": beyond, "beyond_seeds": far}
	if m.gated {
		m.trace["gate"] = m.gateTrace(ungated, seeds, scores, delivered)
	}
	return items
}

// gateTrace records, for L+G's control and for choosing gateScore, every
// walk mid ungated L would deliver: its walk score, whether it names one of
// the query's entities, and whether the gate dropped it.
func (m *memory) gateTrace(ungated []study.Item, seeds []string, scores map[int64]float64, delivered []int64) []map[string]any {
	out := []map[string]any{}
	for _, it := range ungated {
		if it.Kind != "linked" {
			continue
		}
		out = append(out, map[string]any{"id": it.ID, "first": it.SourceFirst, "last": it.SourceLast, "score": scores[it.ID],
			"names_seed": m.graph.namesAny(it.ID, seeds), "dropped": !slices.Contains(delivered, it.ID)})
	}
	return out
}

// list is a ranked list of mids delivered under one kind.
type list struct {
	kind string
	hits []agent.ConversationHit
}

// next pops the list's best mid not yet delivered.
func (l *list) next(seen map[int64]bool) (agent.ConversationHit, bool) {
	for len(l.hits) > 0 {
		h := l.hits[0]
		l.hits = l.hits[1:]
		if !seen[h.ID] {
			return h, true
		}
	}
	return agent.ConversationHit{}, false
}

// take delivers up to slots mids, one from each list in turn; an exhausted
// list yields its turns to the others.
func take(slots int, seen map[int64]bool, lists ...*list) []study.Item {
	var out []study.Item
	for len(out) < slots {
		took := false
		for _, l := range lists {
			if len(out) == slots {
				break
			}
			if h, ok := l.next(seen); ok {
				seen[h.ID] = true
				out = append(out, study.Item{ID: h.ID, Level: "mid", Kind: l.kind, SourceFirst: h.SourceFirst, SourceLast: h.SourceLast, Text: h.Summary})
				took = true
			}
		}
		if !took {
			break
		}
	}
	return out
}

func (m *memory) LastTrace() map[string]any { return m.trace }

func (m *memory) Store() agent.ConversationMemory { return m.store }

func (m *memory) Close() error { return m.store.Close() }

// associating is L-T (#888): recall and memory_search are the shipped
// ones, and the walk answers only memory_associate, a call the model makes
// during work.
type associating struct{ *memory }

func (m associating) Recall(ctx context.Context, task string) ([]study.Item, error) {
	m.trace = nil
	return study.ShippedMemory{Conversation: m.store}.Recall(ctx, task)
}

func (m associating) Search(ctx context.Context, query string, limit int) ([]study.Item, error) {
	m.trace = nil
	return study.ShippedMemory{Conversation: m.store}.Search(ctx, query, limit)
}

// Associate is the walk's ranking of mids for a query the model writes, at
// most limit, each delivered as "linked".
func (m associating) Associate(_ context.Context, query string, limit int) ([]study.Item, error) {
	linked, seeds, steps, _, _ := m.graph.linked(query)
	items := take(limit, map[int64]bool{}, &list{"linked", linked})
	delivered := []int64{}
	for _, it := range items {
		delivered = append(delivered, it.ID)
	}
	m.trace = map[string]any{"seeds": seeds, "steps": steps, "reached": len(linked), "linked": delivered}
	return items, nil
}
