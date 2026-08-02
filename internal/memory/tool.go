package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/garamsh/gagent/internal/tool"
)

// ToolName is the registered name of the memory tool.
const ToolName = "memory"

// toolDescription is the one-sentence summary advertised to model providers,
// matching the memory responsibility in docs/architecture/memory.md.
const toolDescription = "Search the environment's long-term memory."

type memoryTool struct {
	svc Service
}

var _ tool.Tool = (*memoryTool)(nil)

// NewTool returns the memory tool over svc, satisfying the tool.Tool
// contract so it can join the registry.
func NewTool(svc Service) tool.Tool { return &memoryTool{svc: svc} }

// Name implements tool.Tool.
func (t *memoryTool) Name() string { return ToolName }

// Description implements tool.Tool.
func (t *memoryTool) Description() string { return toolDescription }

// InputSchema implements tool.Tool.
func (t *memoryTool) InputSchema() json.RawMessage { return InputSchema() }

// OutputSchema implements tool.Tool.
func (t *memoryTool) OutputSchema() json.RawMessage { return OutputSchema() }

// CallInput is the canonical typed form of the memory tool's input
// schema; the schema/struct consistency test keeps them in lockstep.
// ADR 0015 shrank the tool to the read-only search op; writes flow
// through the writeback block instead.
type CallInput struct {
	Op    string `json:"op"`
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type resultEntry struct {
	ID              int64   `json:"id"`
	Content         string  `json:"content"`
	Type            Type    `json:"type"`
	ImportanceScore float64 `json:"importance_score"`
	Score           float64 `json:"score"`
	Timestamp       string  `json:"timestamp"`
}

// SearchOutput is the result of the search op.
type SearchOutput struct {
	Results []resultEntry `json:"results"`
}

// Call implements tool.Tool. Input has already passed schema validation,
// so the op dispatch in doCall is exhaustive over the schema's single op.
func (t *memoryTool) Call(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	return tool.CallTyped(ctx, t.doCall, input)
}

// doCall dispatches the decoded input to the service op it names.
func (t *memoryTool) doCall(ctx context.Context, in CallInput) (any, error) {
	switch in.Op {
	case "search":
		results, err := t.svc.Search(ctx, in.Query, in.Limit)
		if err != nil {
			return nil, err
		}
		return SearchOutput{Results: entries(results)}, nil
	default:
		return nil, fmt.Errorf("unknown op %q", in.Op)
	}
}

func entries(results []ScoredMemory) []resultEntry {
	out := make([]resultEntry, 0, len(results))
	for _, r := range results {
		out = append(out, resultEntry{
			ID:              r.ID,
			Content:         r.Content,
			Type:            r.Type,
			ImportanceScore: r.ImportanceScore,
			Score:           r.Score,
			Timestamp:       r.Timestamp.UTC().Format(time.RFC3339),
		})
	}
	return out
}
