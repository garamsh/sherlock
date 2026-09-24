package sqlite_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/memory/sqlite"
)

func TestConversation_FailedSummaryLeavesNoSourceOrCheckpoint(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.Open(path)
	require.NoError(t, err)
	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = raw.Exec(`CREATE TRIGGER fail_mid BEFORE INSERT ON conversation_mid BEGIN SELECT RAISE(ABORT,'maintenance failed'); END`)
	require.NoError(t, err)
	require.Error(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: "A source that needs a summary."}, {Role: agent.RoleAssistant, Content: "Answered."}}))
	sources, err := store.Recent(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, sources)
	_, err = raw.Exec(`DROP TRIGGER fail_mid`)
	require.NoError(t, err)
	require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: "A source that needs a summary."}, {Role: agent.RoleAssistant, Content: "Answered."}}))
	sources, err = store.Recent(ctx, 10)
	require.NoError(t, err)
	require.Len(t, sources, 2)
	assert.Equal(t, "A source that needs a summary.", sources[0].Content)
	require.NoError(t, raw.Close())
	require.NoError(t, store.Close())
}

func TestConversation_CredentialsAreNotPersisted(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "memory.db"))
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: "api_key=sk-abcdefghijklmnop and favorite tea is cedar"}, {Role: agent.RoleAssistant, Content: "Bearer secretvalue"}}))
	sources, err := store.Recent(ctx, 10)
	require.NoError(t, err)
	require.Len(t, sources, 2)
	assert.NotContains(t, sources[0].Content, "sk-abcdefghijklmnop")
	assert.Contains(t, sources[0].Content, "favorite tea is cedar")
	assert.NotContains(t, sources[1].Content, "secretvalue")
}

func TestOpenConversation_RefusalDoesNotAddSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = raw.Exec(`CREATE TABLE memories_active (id INTEGER PRIMARY KEY); INSERT INTO memories_active(id) VALUES(7)`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	_, err = sqlite.OpenConversation(path)
	require.ErrorIs(t, err, sqlite.ErrAddressedStore)
	raw, err = sql.Open("sqlite", path)
	require.NoError(t, err)
	defer raw.Close()
	var count int
	require.NoError(t, raw.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='conversation_sources'`).Scan(&count))
	assert.Zero(t, count)
	require.NoError(t, raw.QueryRow(`SELECT count(*) FROM memories_active WHERE id=7`).Scan(&count))
	assert.Equal(t, 1, count)
}

func TestSearch_CorrectionSurvivesRecentGenericMatches(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "memory.db"))
	require.NoError(t, err)
	defer store.Close()
	old := "The secret-free project codename is Blue Cedar. Remember this fact."
	correction := "Correction: the current project codename is Silver Pine. Blue Cedar was the old name and is no longer current."
	for i := 0; i < 67; i++ {
		content := fmt.Sprintf("Filler event %03d: a routine status ping with no project name or policy update.", i-1)
		switch i {
		case 0:
			content = old
		case 1:
			content = correction
		}
		require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{
			{Role: agent.RoleUser, Content: content},
			{Role: agent.RoleAssistant, Content: "42"},
			{Role: agent.RoleAssistant, Content: "Replied via message_send and recorded the answer."},
		}))
	}
	recent, err := store.Recent(ctx, 128)
	require.NoError(t, err)
	require.Len(t, recent, 128)
	require.EqualValues(t, 74, recent[0].ID)
	for _, source := range recent {
		require.NotContains(t, source.Content, "codename")
	}
	tests := []struct {
		name  string
		terms []string
		want  []int64
	}{
		{"generated phrases", []string{"current project codename", "superseded earlier codename", "project codename superseded replaced", "recent project codename change"}, []int64{4, 1, 199, 196, 193}},
		{"discriminative control", []string{"Silver Pine"}, []int64{4}},
		{"paraphrase with lexical overlap", []string{"renamed project codename"}, []int64{1, 4, 199, 196, 193}},
		{"multiple topics", []string{"project codename", "routine status ping"}, []int64{1, 4, 199, 196, 193}},
		{"unsupported fact", []string{"violet submarine"}, nil},
		{"no usable words", []string{"the and ?"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits, err := store.Search(ctx, tt.terms, 5)
			require.NoError(t, err)
			var ids []int64
			for _, hit := range hits {
				short, err := store.Short(ctx, hit.ShortID)
				require.NoError(t, err)
				source, err := store.Source(ctx, short.SourceID)
				require.NoError(t, err)
				ids = append(ids, source.ID)
				if source.ID == 4 {
					assert.Equal(t, correction, source.Content)
				}
			}
			assert.Equal(t, tt.want, ids)
		})
	}
}

func TestSearch_AbsentCorrectionIsNotEvidence(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "memory.db"))
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: "The project codename is Blue Cedar."}}))
	hits, err := store.Search(ctx, []string{"Silver Pine"}, 5)
	require.NoError(t, err)
	assert.Empty(t, hits)
	hits, err = store.Search(ctx, []string{"project codename"}, 5)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Contains(t, hits[0].Summary, "Blue Cedar")
}

func TestSearch_EqualRelevanceUsesNewestWithinLimit(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "memory.db"))
	require.NoError(t, err)
	defer store.Close()
	for i := 0; i < 8; i++ {
		require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: "A repeated project status."}}))
	}
	hits, err := store.Search(ctx, []string{"project status"}, 2)
	require.NoError(t, err)
	require.Len(t, hits, 2)
	assert.EqualValues(t, 8, hits[0].ID)
	assert.EqualValues(t, 7, hits[1].ID)
	hits, err = store.Search(ctx, []string{"project status"}, 0)
	require.NoError(t, err)
	assert.Empty(t, hits)
}
