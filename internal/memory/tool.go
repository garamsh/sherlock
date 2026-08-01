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
const toolDescription = "Store, search, update, compress, and forget entries in the environment's long-term memory."

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
type CallInput struct {
	Op              string   `json:"op"`
	Content         string   `json:"content"`
	Type            Type     `json:"type"`
	ImportanceScore *float64 `json:"importance_score"`
	Source          string   `json:"source"`
	Query           string   `json:"query"`
	Limit           int      `json:"limit"`
	ID              int64    `json:"id"`
	IDs             []int64  `json:"ids"`
	Threshold       float64  `json:"threshold"`
}

// StoreOutput is the result of the store op.
type StoreOutput struct {
	ID         int64   `json:"id"`
	EvictedIDs []int64 `json:"evicted_ids"`
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

// UpdateOutput is the result of the update op.
type UpdateOutput struct {
	ID           int64 `json:"id"`
	SupersededID int64 `json:"superseded_id"`
}

// CompressOutput is the result of the compress op.
type CompressOutput struct {
	ParentID    int64   `json:"parent_id"`
	ArchivedIDs []int64 `json:"archived_ids"`
}

// ForgetOutput is the result of the forget op.
type ForgetOutput struct {
	ArchivedIDs []int64 `json:"archived_ids"`
}

// Call implements tool.Tool. Input has already passed schema validation,
// so the op dispatch in doCall is exhaustive over the schema's five ops.
func (t *memoryTool) Call(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	return tool.CallTyped(ctx, t.doCall, input)
}

// doCall dispatches the decoded input to the service op it names. The
// result type varies per op, so the output is any; the schema/struct
// consistency test pins each variant to the output schema's oneOf.
func (t *memoryTool) doCall(ctx context.Context, in CallInput) (any, error) {
	switch in.Op {
	case "store":
		res, err := t.svc.Store(ctx, StoreInput{
			Content:         in.Content,
			Type:            in.Type,
			ImportanceScore: deref(in.ImportanceScore),
			Source:          in.Source,
		})
		if err != nil {
			return nil, err
		}
		return StoreOutput{ID: res.ID, EvictedIDs: nonNil(res.EvictedIDs)}, nil
	case "search":
		results, err := t.svc.Search(ctx, in.Query, in.Limit)
		if err != nil {
			return nil, err
		}
		return SearchOutput{Results: entries(results)}, nil
	case "update":
		res, err := t.svc.Update(ctx, UpdateInput{
			ID:              in.ID,
			Content:         in.Content,
			ImportanceScore: in.ImportanceScore,
		})
		if err != nil {
			return nil, err
		}
		return UpdateOutput(res), nil
	case "compress":
		res, err := t.svc.Compress(ctx, in.IDs)
		if err != nil {
			return nil, err
		}
		return CompressOutput{ParentID: res.ParentID, ArchivedIDs: nonNil(res.ArchivedIDs)}, nil
	case "forget":
		archived, err := t.svc.Forget(ctx, in.Threshold)
		if err != nil {
			return nil, err
		}
		return ForgetOutput{ArchivedIDs: nonNil(archived)}, nil
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

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// nonNil keeps array fields serializing as [] rather than null so the
// output schema stays honest.
func nonNil(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}
	return ids
}
