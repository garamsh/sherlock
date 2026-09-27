package main

import (
	"context"
	"strings"

	"github.com/garamsh/sherlock/docs/research/chained-recall/study"
	"github.com/garamsh/sherlock/internal/agent"
)

// contextLabel introduces a mid's context on recall, apart from its
// summary (../../../README.md §C).
const contextLabel = "Context when written: "

// memory is the shipped recall and search over C's store, each mid shown
// with its context.
type memory struct {
	shipped  study.ShippedMemory
	contexts map[int64]string
}

func (m memory) Recall(ctx context.Context, task string) ([]study.Item, error) {
	items, err := m.shipped.Recall(ctx, task)
	return m.labelled(items), err
}

func (m memory) Search(ctx context.Context, query string, limit int) ([]study.Item, error) {
	items, err := m.shipped.Search(ctx, query, limit)
	return m.labelled(items), err
}

// labelled appends each mid's context, on its own labelled line, after its
// summary.
func (m memory) labelled(items []study.Item) []study.Item {
	for i, it := range items {
		if c := m.contexts[it.ID]; c != "" {
			items[i].Text = strings.TrimSpace(it.Text) + "\n" + contextLabel + c
		}
	}
	return items
}

func (m memory) Store() agent.ConversationMemory { return m.shipped.Store() }
func (m memory) Close() error                    { return m.shipped.Close() }
