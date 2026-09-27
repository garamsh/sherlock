package study

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/garamsh/sherlock/internal/agent"
)

// Recalled renders items as the production recall renders mids
// (internal/agent/memory/conversation.go): the summary followed by a label
// naming the item and its source interval. A mid keeps its bare ID, as in
// production; another level's ID is prefixed with the level.
func Recalled(items []Item) []agent.RecalledMemory {
	out := make([]agent.RecalledMemory, 0, len(items))
	for _, it := range items {
		if it.Level == "notice" {
			// A notice an arm's runtime adds (L+chk, #896) is no memory:
			// it has no ID or interval to label.
			out = append(out, agent.RecalledMemory{ID: "notice", Content: it.Text, Age: it.Kind})
			continue
		}
		id := strconv.FormatInt(it.ID, 10)
		if it.Level != "mid" {
			id = it.Level + ":" + id
		}
		label := fmt.Sprintf("[%s:%d sources:%d-%d; summary, not verified source text]", it.Level, it.ID, it.SourceFirst, it.SourceLast)
		if it.Label != "" {
			label = it.Label
		}
		out = append(out, agent.RecalledMemory{
			ID:      id,
			Content: strings.TrimSpace(it.Text) + " " + label,
			Age:     it.Kind,
		})
	}
	return out
}

// ContextChars is how many characters the items occupy in the recall
// block, counting each line as the session writes it (frameContext).
func ContextChars(items []Item) int {
	n := 0
	for _, m := range Recalled(items) {
		n += len([]rune(fmt.Sprintf("- [%s] (%s): %s\n", m.ID, m.Age, m.Content)))
	}
	return n
}

// recaller is the live session's Recaller over an arm's memory: the arm's
// items, then the pending source previews production appends.
type recaller struct{ memory Memory }

func (r recaller) Recall(ctx context.Context, task string) (agent.Recalled, error) {
	items, err := r.memory.Recall(ctx, task)
	if err != nil {
		return agent.Recalled{}, err
	}
	out := Recalled(items)
	pending, err := r.memory.Store().PendingSources(ctx)
	if err != nil {
		return agent.Recalled{}, err
	}
	for _, source := range pending {
		preview := []rune(source.Content)
		if len(preview) > 2000 {
			preview = preview[:2000]
		}
		out = append(out, agent.RecalledMemory{ID: fmt.Sprintf("source:%d", source.ID), Content: string(preview) + fmt.Sprintf(" [source:%d; pending %s message preview; inspect source for full text]", source.ID, source.Role), Age: "pending"})
	}
	if len(out) == 0 {
		return agent.Recalled{Outcome: agent.RecallEmpty}, nil
	}
	return agent.Recalled{Memories: out, Outcome: agent.RecallAddressed}, nil
}

// conversation is the live session's conversation memory: the arm's
// production store, with memory_search answered by the arm.
type conversation struct {
	agent.ConversationMemory
	memory Memory
}

func (c conversation) Search(ctx context.Context, query string, limit int) ([]agent.ConversationHit, error) {
	items, err := c.memory.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]agent.ConversationHit, 0, len(items))
	for _, it := range items {
		summary := it.Text
		if it.Level != "mid" {
			summary = it.Level + ": " + summary
		}
		out = append(out, agent.ConversationHit{ID: it.ID, Summary: summary, SourceFirst: it.SourceFirst, SourceLast: it.SourceLast})
	}
	return out, nil
}
