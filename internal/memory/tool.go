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

// inputSchema validates agent input per operation (secret-holding trust
// domain: structured, schema-validated input only).
var inputSchema = json.RawMessage(`{
	"$schema": "https://json-schema.org/draft/2020-12/schema",
	"oneOf": [
		{
			"type": "object",
			"properties": {
				"op": {"const": "store"},
				"content": {"type": "string", "minLength": 1},
				"type": {"enum": ["episodic", "semantic"]},
				"importance_score": {"type": "number", "minimum": 0, "maximum": 1},
				"source": {"type": "string", "minLength": 1}
			},
			"required": ["op", "content", "type", "importance_score", "source"],
			"additionalProperties": false
		},
		{
			"type": "object",
			"properties": {
				"op": {"const": "search"},
				"query": {"type": "string", "minLength": 1},
				"limit": {"type": "integer", "minimum": 1}
			},
			"required": ["op", "query"],
			"additionalProperties": false
		},
		{
			"type": "object",
			"properties": {
				"op": {"const": "update"},
				"id": {"type": "integer", "minimum": 1},
				"content": {"type": "string", "minLength": 1},
				"importance_score": {"type": "number", "minimum": 0, "maximum": 1}
			},
			"required": ["op", "id", "content"],
			"additionalProperties": false
		},
		{
			"type": "object",
			"properties": {
				"op": {"const": "compress"},
				"ids": {"type": "array", "items": {"type": "integer", "minimum": 1}, "minItems": 1}
			},
			"required": ["op", "ids"],
			"additionalProperties": false
		},
		{
			"type": "object",
			"properties": {
				"op": {"const": "forget"},
				"threshold": {"type": "number", "exclusiveMinimum": 0}
			},
			"required": ["op"],
			"additionalProperties": false
		}
	]
}`)

var outputSchema = json.RawMessage(`{
	"$schema": "https://json-schema.org/draft/2020-12/schema",
	"oneOf": [
		{
			"type": "object",
			"properties": {
				"id": {"type": "integer"},
				"evicted_ids": {"type": "array", "items": {"type": "integer"}}
			},
			"required": ["id", "evicted_ids"]
		},
		{
			"type": "object",
			"properties": {
				"results": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"id": {"type": "integer"},
							"content": {"type": "string"},
							"type": {"enum": ["episodic", "semantic"]},
							"importance_score": {"type": "number"},
							"score": {"type": "number"},
							"timestamp": {"type": "string"}
						},
						"required": ["id", "content", "type", "importance_score", "score", "timestamp"]
					}
				}
			},
			"required": ["results"]
		},
		{
			"type": "object",
			"properties": {
				"id": {"type": "integer"},
				"superseded_id": {"type": "integer"}
			},
			"required": ["id", "superseded_id"]
		},
		{
			"type": "object",
			"properties": {
				"parent_id": {"type": "integer"},
				"archived_ids": {"type": "array", "items": {"type": "integer"}}
			},
			"required": ["parent_id", "archived_ids"]
		},
		{
			"type": "object",
			"properties": {
				"archived_ids": {"type": "array", "items": {"type": "integer"}}
			},
			"required": ["archived_ids"]
		}
	]
}`)

type memoryTool struct {
	svc Service
}

var _ tool.Tool = (*memoryTool)(nil)

// NewTool returns the memory tool over svc, satisfying the tool.Tool
// contract so it can join the registry.
func NewTool(svc Service) tool.Tool { return &memoryTool{svc: svc} }

// Name implements tool.Tool.
func (t *memoryTool) Name() string { return ToolName }

// InputSchema implements tool.Tool.
func (t *memoryTool) InputSchema() json.RawMessage { return inputSchema }

// OutputSchema implements tool.Tool.
func (t *memoryTool) OutputSchema() json.RawMessage { return outputSchema }

type callInput struct {
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

type storeOutput struct {
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

type searchOutput struct {
	Results []resultEntry `json:"results"`
}

type updateOutput struct {
	ID           int64 `json:"id"`
	SupersededID int64 `json:"superseded_id"`
}

type compressOutput struct {
	ParentID    int64   `json:"parent_id"`
	ArchivedIDs []int64 `json:"archived_ids"`
}

type forgetOutput struct {
	ArchivedIDs []int64 `json:"archived_ids"`
}

// Call implements tool.Tool. Input has already passed schema validation,
// so the op dispatch below is exhaustive over the schema's five ops.
func (t *memoryTool) Call(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var in callInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}
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
		return marshal(storeOutput{ID: res.ID, EvictedIDs: nonNil(res.EvictedIDs)})
	case "search":
		results, err := t.svc.Search(ctx, in.Query, in.Limit)
		if err != nil {
			return nil, err
		}
		return marshal(searchOutput{Results: entries(results)})
	case "update":
		res, err := t.svc.Update(ctx, UpdateInput{
			ID:              in.ID,
			Content:         in.Content,
			ImportanceScore: in.ImportanceScore,
		})
		if err != nil {
			return nil, err
		}
		return marshal(updateOutput{ID: res.ID, SupersededID: res.SupersededID})
	case "compress":
		res, err := t.svc.Compress(ctx, in.IDs)
		if err != nil {
			return nil, err
		}
		return marshal(compressOutput{ParentID: res.ParentID, ArchivedIDs: nonNil(res.ArchivedIDs)})
	case "forget":
		archived, err := t.svc.Forget(ctx, in.Threshold)
		if err != nil {
			return nil, err
		}
		return marshal(forgetOutput{ArchivedIDs: nonNil(archived)})
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

func marshal(v any) (json.RawMessage, error) {
	out, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return out, nil
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
