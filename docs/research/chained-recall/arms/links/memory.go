package main

import (
	"context"

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
	linked, seeds, steps := m.graph.linked(query)
	items := take(slots, seen, &list{"historical", similar}, &list{"linked", linked})
	inSimilar := map[int64]bool{}
	for _, h := range similar {
		inSimilar[h.ID] = true
	}
	delivered, beyond, far := []int64{}, []int64{}, []int64{}
	for _, it := range items {
		if it.Kind != "linked" {
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
	return items
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
