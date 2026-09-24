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
				if json.Unmarshal(call.Input, &v) == nil && len(v.Phrases) > 0 {
					terms = v.Phrases
					if len(terms) > 4 {
						terms = terms[:4]
					}
				}
				break
			}
		}
	}
	hits, err := r.Store.Search(ctx, terms, maxRecalled)
	if err != nil {
		return agent.Recalled{}, err
	}
	if len(hits) == 0 {
		return agent.Recalled{Outcome: agent.RecallEmpty}, nil
	}
	out := make([]agent.RecalledMemory, 0, len(hits))
	for _, hit := range hits {
		out = append(out, agent.RecalledMemory{ID: strconv.FormatInt(hit.ID, 10), Content: strings.TrimSpace(hit.Summary) + fmt.Sprintf(" [short:%d]", hit.ShortID), Age: "historical"})
	}
	return agent.Recalled{Memories: out, Outcome: agent.RecallAddressed}, nil
}
