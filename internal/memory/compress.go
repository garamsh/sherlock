package memory

import (
	"context"
	"fmt"
)

// CompressSource is the source recorded on compressed parent memories.
const CompressSource = "compress"

// CompressResult reports the new semantic parent and the archived
// episodic originals.
type CompressResult struct {
	ParentID    int64
	ArchivedIDs []int64
}

// Compress implements the compress operation: summarize a set of
// episodic memories into one semantic parent, originals archived.
func (s *service) Compress(ctx context.Context, ids []int64) (CompressResult, error) {
	if len(ids) == 0 {
		return CompressResult{}, fmt.Errorf("%w: no ids to compress", ErrInvalidInput)
	}
	originals := make([]Memory, 0, len(ids))
	contents := make([]string, 0, len(ids))
	for _, id := range ids {
		m, err := s.store.Get(ctx, id)
		if err != nil {
			return CompressResult{}, err
		}
		if m.Type != TypeEpisodic {
			return CompressResult{}, fmt.Errorf("%w: id %d is %s", ErrNotEpisodic, id, m.Type)
		}
		originals = append(originals, m)
		contents = append(contents, m.Content)
	}
	summary, err := s.summarizer.Summarize(ctx, contents)
	if err != nil {
		return CompressResult{}, fmt.Errorf("summarize: %w", err)
	}
	emb, err := s.embedder.Embed(ctx, summary)
	if err != nil {
		return CompressResult{}, fmt.Errorf("embed summary: %w", err)
	}
	importance := originals[0].ImportanceScore
	for _, m := range originals[1:] {
		importance = max(importance, m.ImportanceScore)
	}
	now := s.clock()
	parentID, err := s.store.Insert(ctx, Memory{
		Content:         summary,
		Embedding:       emb,
		Type:            TypeSemantic,
		ImportanceScore: importance,
		Timestamp:       now,
		Source:          CompressSource,
	})
	if err != nil {
		return CompressResult{}, fmt.Errorf("insert parent: %w", err)
	}
	for _, m := range originals {
		if err := s.store.Archive(ctx, m.ID, parentID, now); err != nil {
			return CompressResult{}, fmt.Errorf("archive original %d: %w", m.ID, err)
		}
	}
	archived := make([]int64, 0, len(originals))
	for _, m := range originals {
		archived = append(archived, m.ID)
	}
	return CompressResult{ParentID: parentID, ArchivedIDs: archived}, nil
}
