package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/garamsh/sherlock/internal/tool"
)

// MemorySearchTool is the reserved call that ranks mid episodes by
// similarity to a query the model writes during its turn, beside the
// episodes the entity walk reaches from the names the query contains.
// It is the only way older conversation reaches the model.
const MemorySearchTool = "memory_search"

// maxSearchResults bounds one search.
const maxSearchResults = 5

// MemorySearchDescriptor is the reserved search call as the model sees it.
func MemorySearchDescriptor() tool.Descriptor {
	return tool.Descriptor{Name: MemorySearchTool, Description: "Search older conversation for mid episodes similar to a query. Write the query as a standalone statement of what you need, resolving references such as \"that\" or \"what I said earlier\" from the conversation and your memory files. Results are summaries with source intervals, not verified text; read exact wording with memory_inspect.", InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1}},"required":["query"],"additionalProperties":false}`)}
}

// searchHit is one search result as the model reads it.
type searchHit struct {
	ID          int64  `json:"mid"`
	Summary     string `json:"summary"`
	SourceFirst int64  `json:"source_first"`
	SourceLast  int64  `json:"source_last"`
}

func searchMemory(ctx context.Context, store ConversationMemory, input json.RawMessage) (string, error) {
	var request struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return "", err
	}
	query := strings.TrimSpace(request.Query)
	if query == "" {
		return "", errors.New("query must not be empty")
	}
	similar, err := store.Search(ctx, query, maxSearchResults)
	if err != nil {
		return "", err
	}
	linked, err := store.Linked(ctx, query)
	if err != nil {
		return "", err
	}
	hits := Interleave(maxSearchResults, map[int64]bool{}, similar, linked)
	out := make([]searchHit, 0, len(hits))
	for _, hit := range hits {
		out = append(out, searchHit{ID: hit.ID, Summary: strings.TrimSpace(hit.Summary), SourceFirst: hit.SourceFirst, SourceLast: hit.SourceLast})
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
