package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

func TestAccumulate_RollbackDoesNotAdvanceCheckpointOrIndex(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.OpenConversation(path)
	require.NoError(t, err)
	defer store.Close()
	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer raw.Close()
	_, err = raw.Exec(`CREATE TRIGGER fail_mid AFTER INSERT ON conversation_mid BEGIN SELECT RAISE(ABORT,'maintenance failed'); END`)
	require.NoError(t, err)
	require.Error(t, store.RecordTurn(ctx, episodeSources("searchable original"), firstSummary{}))
	pending, err := store.PendingSources(ctx)
	require.NoError(t, err)
	require.Len(t, pending, 8)
	mids, err := store.RecentMid(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, mids)
	hits, err := store.Search(ctx, []string{"searchable"}, 10)
	require.NoError(t, err)
	assert.Empty(t, hits)
	_, err = raw.Exec(`DROP TRIGGER fail_mid`)
	require.NoError(t, err)
	require.NoError(t, store.Accumulate(ctx, firstSummary{}))
	hits, err = store.Search(ctx, []string{"searchable"}, 10)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.EqualValues(t, 1, hits[0].SourceFirst)
	assert.EqualValues(t, 8, hits[0].SourceLast)
}

func TestAccumulate_ModelCallHoldsNoWriteTransaction(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.OpenConversation(path)
	require.NoError(t, err)
	defer store.Close()
	raw, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(1)")
	require.NoError(t, err)
	defer raw.Close()
	summarize := summaryFunc(func(_ context.Context, updates []string) (string, error) {
		_, err := raw.Exec(`CREATE TABLE concurrent_writer (id INTEGER)`)
		if err != nil {
			return "", err
		}
		return updates[0], nil
	})
	require.NoError(t, store.RecordTurn(ctx, episodeSources("Model call without write lock"), summarize))
}
