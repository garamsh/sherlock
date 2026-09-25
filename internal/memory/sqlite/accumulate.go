package sqlite

import (
	"context"
	"errors"
	"fmt"

	"github.com/garamsh/sherlock/internal/agent"
)

// batchMessages is how many completed visible messages make one mid episode.
const batchMessages = 4

// Accumulate creates one immutable mid episode per full cross-turn message batch.
// The source checkpoint commits with the episode, after remote synthesis returns;
// embedding follows once no full batch remains.
func (s *Store) Accumulate(ctx context.Context, summarizer agent.ConversationSummarizer) error {
	for {
		var lastSource int64
		if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(source_last),0) FROM conversation_mid`).Scan(&lastSource); err != nil {
			return fmt.Errorf("read episode checkpoint: %v", err)
		}
		sources, err := s.sourcesAfter(ctx, lastSource)
		if err != nil {
			return err
		}
		if len(sources) < batchMessages {
			return s.embedMids(ctx)
		}
		updates := make([]string, 0, len(sources))
		for _, source := range sources {
			updates = append(updates, string(source.Role)+": "+source.Content)
		}
		summary, err := summarize(ctx, summarizer, updates)
		if err != nil {
			return err
		}
		// A concurrent writer must not publish a second episode for the same batch.
		result, err := s.db.ExecContext(ctx, `INSERT INTO conversation_mid(source_first,source_last,summary)
 SELECT ?,?,? WHERE COALESCE((SELECT MAX(source_last) FROM conversation_mid),0)=?`, sources[0].ID, sources[len(sources)-1].ID, summary, lastSource)
		if err != nil {
			return fmt.Errorf("commit episode checkpoint: %v", err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read episode commit result: %v", err)
		}
		if count != 1 {
			return errors.New("episode checkpoint changed; retry maintenance")
		}
	}
}

func (s *Store) sourcesAfter(ctx context.Context, after int64) ([]agent.SourceMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,external_id,role,content FROM conversation_sources WHERE id>? ORDER BY id LIMIT ?`, after, batchMessages)
	if err != nil {
		return nil, fmt.Errorf("read pending sources: %v", err)
	}
	defer rows.Close()
	var out []agent.SourceMessage
	for rows.Next() {
		var source agent.SourceMessage
		if err := rows.Scan(&source.ID, &source.ExternalID, &source.Role, &source.Content); err != nil {
			return nil, fmt.Errorf("read pending sources: %v", err)
		}
		out = append(out, source)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read pending sources: %v", err)
	}
	return out, nil
}

// PendingSources returns a bounded page of sources beyond the last mid checkpoint.
func (s *Store) PendingSources(ctx context.Context) ([]agent.SourceMessage, error) {
	var lastSource int64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(source_last),0) FROM conversation_mid`).Scan(&lastSource); err != nil {
		return nil, fmt.Errorf("read pending sources: %v", err)
	}
	return s.sourcesAfter(ctx, lastSource)
}

// embedMids stores an embedding for every mid episode that lacks one: a new
// episode, one whose embedding failed before, or one written before embeddings.
func (s *Store) embedMids(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id,summary FROM conversation_mid WHERE embedding IS NULL ORDER BY id`)
	if err != nil {
		return fmt.Errorf("read unembedded episodes: %v", err)
	}
	var mids []agent.ConversationHit
	for rows.Next() {
		var mid agent.ConversationHit
		if err := rows.Scan(&mid.ID, &mid.Summary); err != nil {
			rows.Close()
			return fmt.Errorf("read unembedded episodes: %v", err)
		}
		mids = append(mids, mid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read unembedded episodes: %v", err)
	}
	for _, mid := range mids {
		vector, err := s.embed(ctx, mid.Summary)
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE conversation_mid SET embedding=? WHERE id=?`, encodeVector(vector), mid.ID); err != nil {
			return fmt.Errorf("store episode embedding: %v", err)
		}
	}
	return nil
}
