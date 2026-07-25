package memory

import (
	"context"
	"fmt"
)

// UpdateInput carries a superseding revision of a memory. A nil
// ImportanceScore inherits the old memory's value.
type UpdateInput struct {
	ID              int64
	Content         string
	ImportanceScore *float64
}

// UpdateResult reports the new version and the archived predecessor.
type UpdateResult struct {
	ID           int64
	SupersededID int64
}

// Update implements the update operation: supersede, never overwrite.
func (s *service) Update(ctx context.Context, in UpdateInput) (UpdateResult, error) {
	old, err := s.store.Get(ctx, in.ID)
	if err != nil {
		return UpdateResult{}, err
	}
	importance := old.ImportanceScore
	if in.ImportanceScore != nil {
		importance = *in.ImportanceScore
	}
	if err := validateNew(in.Content, old.Type, importance); err != nil {
		return UpdateResult{}, err
	}
	emb, err := s.embedder.Embed(ctx, in.Content)
	if err != nil {
		return UpdateResult{}, fmt.Errorf("embed: %w", err)
	}
	now := s.clock()
	newID, err := s.store.Insert(ctx, Memory{
		Content:         in.Content,
		Embedding:       emb,
		Type:            old.Type,
		ImportanceScore: importance,
		Timestamp:       now,
		Source:          old.Source,
	})
	if err != nil {
		return UpdateResult{}, fmt.Errorf("insert: %w", err)
	}
	if err := s.store.Archive(ctx, in.ID, newID, now); err != nil {
		return UpdateResult{}, fmt.Errorf("archive superseded %d: %w", in.ID, err)
	}
	return UpdateResult{ID: newID, SupersededID: in.ID}, nil
}
