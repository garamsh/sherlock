package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/tool"
)

const maxRecalled = 5

// ConversationRecall asks the model for search wording, then reads bounded
// candidates from the source-linked index. Search wording is a hint, never
// evidence; an empty result remains empty.
type ConversationRecall struct {
	Store agent.ConversationMemory
	Model agent.Model
}

func (r ConversationRecall) Recall(ctx context.Context, task string) (agent.Recalled, error) {
	terms := []string{task}
	if r.Model != nil {
		resp, err := r.Model.Chat(ctx, []agent.Message{
			{Role: agent.RoleSystem, Content: "Give up to four short search phrases for the older conversation that could answer this task. Include alternate wording and each topic separately. Call memory_search_terms; do not answer the task."},
			{Role: agent.RoleUser, Content: task},
		}, []tool.Descriptor{{Name: "memory_search_terms", Description: "Supply search phrases for older conversation", InputSchema: json.RawMessage(`{"type":"object","properties":{"phrases":{"type":"array","items":{"type":"string"}}},"required":["phrases"],"additionalProperties":false}`)}})
		if err != nil {
			return agent.Recalled{}, fmt.Errorf("search wording: %w", err)
		}
		for _, call := range resp.ToolCalls {
			if call.Name == "memory_search_terms" {
				var v struct {
					Phrases []string `json:"phrases"`
				}
				if json.Unmarshal(call.Input, &v) == nil && len(agent.SearchTokens(v.Phrases[:min(4, len(v.Phrases))])) > 0 {
					terms = v.Phrases
					if len(terms) > 4 {
						terms = terms[:4]
					}
				}
				break
			}
		}
	}
	recent, err := r.Store.RecentMid(ctx, 2)
	if err != nil {
		return agent.Recalled{}, err
	}
	hits, err := r.Store.Search(ctx, terms, maxRecalled+len(recent))
	if err != nil {
		return agent.Recalled{}, err
	}
	out := make([]agent.RecalledMemory, 0, maxRecalled+len(recent))
	seen := map[int64]bool{}
	for i, hit := range append(recent, hits...) {
		if seen[hit.ID] {
			continue
		}
		seen[hit.ID] = true
		age := "historical"
		if i < len(recent) {
			age = "recent"
		}
		out = append(out, agent.RecalledMemory{ID: strconv.FormatInt(hit.ID, 10), Content: strings.TrimSpace(hit.Summary) + fmt.Sprintf(" [mid:%d sources:%d-%d; summary, not verified source text]", hit.ID, hit.SourceFirst, hit.SourceLast), Age: age})
		if len(out) == maxRecalled+len(recent) {
			break
		}
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
