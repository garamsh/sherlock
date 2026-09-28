package memory

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/garamsh/sherlock/internal/agent"
)

const maxRecalled = 5

// ConversationRecall reads the newest mid episodes, then fills its other slots
// from the ones whose summaries are most similar to the task and the ones the
// entity walk reaches from the names it contains, with no model call. A
// candidate is a hint, never evidence; an empty result remains empty.
type ConversationRecall struct {
	Store agent.ConversationMemory
}

func (r ConversationRecall) Recall(ctx context.Context, task string) (agent.Recalled, error) {
	recent, err := r.Store.RecentMid(ctx, 2)
	if err != nil {
		return agent.Recalled{}, err
	}
	similar, err := r.Store.Search(ctx, task, maxRecalled+len(recent))
	if err != nil {
		return agent.Recalled{}, err
	}
	linked, err := r.Store.Linked(ctx, task)
	if err != nil {
		return agent.Recalled{}, err
	}
	out := make([]agent.RecalledMemory, 0, maxRecalled+len(recent))
	seen := map[int64]bool{}
	for _, hit := range recent {
		seen[hit.ID] = true
		out = append(out, recalledMid(hit, "recent"))
	}
	for _, episode := range agent.Interleave(maxRecalled, seen, similar, linked) {
		age := "historical"
		if episode.Linked {
			age = "linked"
		}
		out = append(out, recalledMid(episode.ConversationHit, age))
	}
	pending, err := r.Store.PendingSources(ctx)
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

func recalledMid(hit agent.ConversationHit, age string) agent.RecalledMemory {
	return agent.RecalledMemory{ID: strconv.FormatInt(hit.ID, 10), Content: strings.TrimSpace(hit.Summary) + fmt.Sprintf(" [mid:%d sources:%d-%d; summary, not verified source text]", hit.ID, hit.SourceFirst, hit.SourceLast), Age: age}
}
