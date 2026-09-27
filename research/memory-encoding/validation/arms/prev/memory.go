package main

import (
	"context"
	"math"

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

// everyMid asks the production search for its whole ranking, which orders
// the thread list.
const everyMid = math.MaxInt32

// memory is an opened P store: the production store under it, answering
// recency and similarity, and the links, in both directions.
type memory struct {
	store *sqlite.Store
	links map[int64][]int64
	trace map[string]any
}

// Recall returns the two newest mids, then fills the shipped recall's five
// historical slots from the similar list and the thread list in turn, so
// linked mids take similar ones' places rather than adding context.
func (m *memory) Recall(ctx context.Context, task string) ([]study.Item, error) {
	recent, err := m.store.RecentMid(ctx, recentMids)
	if err != nil {
		return nil, err
	}
	ranked, err := m.store.Search(ctx, task, everyMid)
	if err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	var items []study.Item
	for _, h := range recent {
		seen[h.ID] = true
		items = append(items, item(h, "recent"))
	}
	return append(items, m.alternate(historicalSlots, seen, ranked, historicalSlots+len(recent))...), nil
}

// Search answers memory_search the same way: limit slots, taken in turn
// from the similar list and the thread list.
func (m *memory) Search(ctx context.Context, query string, limit int) ([]study.Item, error) {
	ranked, err := m.store.Search(ctx, query, everyMid)
	if err != nil {
		return nil, err
	}
	return m.alternate(limit, map[int64]bool{}, ranked, limit), nil
}

// alternate fills slots from the similar list, the first similar mids of
// the ranking, and the thread list in turn. The thread list's next mid is,
// among the linked neighbours of the mids delivered in these slots so far,
// the one the ranking puts first. The recent mids are delivered by age,
// not by the task, so their neighbours are not followed. The trace records
// the thread mids delivered and those beyond the similar list.
func (m *memory) alternate(slots int, seen map[int64]bool, ranked []agent.ConversationHit, similar int) []study.Item {
	rank := map[int64]int{}
	for i, h := range ranked {
		rank[h.ID] = i
	}
	frontier := map[int64]bool{}
	var items []study.Item
	thread, beyond := []int64{}, []int64{}
	deliver := func(h agent.ConversationHit, kind string) {
		seen[h.ID] = true
		items = append(items, item(h, kind))
		for _, n := range m.links[h.ID] {
			frontier[n] = true
		}
	}
	next := 0
	for len(items) < slots {
		took := false
		for next < min(similar, len(ranked)) && seen[ranked[next].ID] {
			next++
		}
		if next < min(similar, len(ranked)) {
			deliver(ranked[next], "historical")
			took = true
		}
		if len(items) == slots {
			break
		}
		best := -1
		for n := range frontier {
			if r, ok := rank[n]; ok && !seen[n] && (best < 0 || r < best) {
				best = r
			}
		}
		if best >= 0 {
			deliver(ranked[best], "thread")
			thread = append(thread, ranked[best].ID)
			if best >= similar {
				beyond = append(beyond, ranked[best].ID)
			}
			took = true
		}
		if !took {
			break
		}
	}
	m.trace = map[string]any{"thread": thread, "beyond_similar": beyond}
	return items
}

func item(h agent.ConversationHit, kind string) study.Item {
	return study.Item{ID: h.ID, Level: "mid", Kind: kind, SourceFirst: h.SourceFirst, SourceLast: h.SourceLast, Text: h.Summary}
}

func (m *memory) LastTrace() map[string]any { return m.trace }

func (m *memory) Store() agent.ConversationMemory { return m.store }

func (m *memory) Close() error { return m.store.Close() }
