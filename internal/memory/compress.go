package memory

import (
	"context"
	"fmt"
	"strings"
)

// CompressSource is the source recorded on compressed parent memories.
const CompressSource = "compress"

// CompressResult reports the new semantic parents and the archived
// episodic originals. Large clusters split into multiple parents each at
// most SummaryMaxLen so every byte of the originals stays inside the
// searchable index; a small cluster yields a single-element ParentIDs.
type CompressResult struct {
	ParentIDs   []int64
	ArchivedIDs []int64
}

// chunkSummary splits a summarizer's joined output into chunks each at
// most maxLen bytes, preferring to break at separator boundaries so a
// single content piece is never split mid-token. A piece longer than
// maxLen on its own is hard-capped; the chunker never drops content.
// Empty input yields no chunks.
func chunkSummary(text string, maxLen int, sep string) []string {
	if text == "" {
		return nil
	}
	if maxLen <= 0 || len(text) <= maxLen {
		return []string{text}
	}
	pieces := strings.Split(text, sep)
	var chunks []string
	var current string
	for _, piece := range pieces {
		if len(piece) >= maxLen {
			if current != "" {
				chunks = append(chunks, current)
				current = ""
			}
			chunks = append(chunks, piece[:maxLen])
			continue
		}
		if current == "" {
			current = piece
			continue
		}
		candidate := current + sep + piece
		if len(candidate) <= maxLen {
			current = candidate
			continue
		}
		chunks = append(chunks, current)
		current = piece
	}
	if current != "" {
		chunks = append(chunks, current)
	}
	return chunks
}

// Compress implements the compress operation: summarize a set of
// episodic memories into one or more semantic parents, originals
// archived. The joined summary is chunked into parents each at most
// SummaryMaxLen so every byte of the originals stays searchable, no
// matter how large the cluster.
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
	// Compress rewrites memories the writeback already curated, so it
	// keeps the text whichever arm produced it; provenance gates the
	// context-overflow write, where the content is not yet curated.
	chunks := chunkSummary(summary.Text, SummaryMaxLen, ChunkSeparator)
	if len(chunks) == 0 {
		return CompressResult{}, fmt.Errorf("summarize: %w: empty output", ErrInvalidInput)
	}
	importance := originals[0].ImportanceScore
	for _, m := range originals[1:] {
		importance = max(importance, m.ImportanceScore)
	}
	now := s.clock()
	parentIDs := make([]int64, 0, len(chunks))
	for _, chunk := range chunks {
		emb, err := s.embedder.Embed(ctx, chunk)
		if err != nil {
			return CompressResult{}, fmt.Errorf("embed chunk: %w", err)
		}
		parentID, err := s.store.Insert(ctx, Memory{
			Content:         chunk,
			Embedding:       emb,
			Type:            TypeSemantic,
			ImportanceScore: importance,
			Timestamp:       now,
			Source:          CompressSource,
		})
		if err != nil {
			return CompressResult{}, fmt.Errorf("insert parent: %w", err)
		}
		parentIDs = append(parentIDs, parentID)
	}
	for _, m := range originals {
		if err := s.store.Archive(ctx, m.ID, parentIDs[0], now); err != nil {
			return CompressResult{}, fmt.Errorf("archive original %d: %w", m.ID, err)
		}
	}
	archived := make([]int64, 0, len(originals))
	for _, m := range originals {
		archived = append(archived, m.ID)
	}
	return CompressResult{ParentIDs: parentIDs, ArchivedIDs: archived}, nil
}
