package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
)

var now = time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)

func openStore(t *testing.T) (*sqlite.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memory.db")
	st, err := sqlite.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, st.Close()) })
	return st, path
}

func sample(content string) memory.Memory {
	return memory.Memory{
		Content:         content,
		Embedding:       []float32{0.5, -0.5, 0.25},
		Type:            memory.TypeEpisodic,
		ImportanceScore: 0.7,
		Timestamp:       now,
		Source:          "test",
	}
}

func TestStore_InsertGetRoundTrip(t *testing.T) {
	st, _ := openStore(t)
	ctx := context.Background()

	id, err := st.Insert(ctx, sample("the cat sat"))
	require.NoError(t, err)
	require.Positive(t, id)

	got, err := st.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, id, got.ID)
	assert.Equal(t, "the cat sat", got.Content)
	assert.Equal(t, memory.TypeEpisodic, got.Type)
	assert.InDelta(t, 0.7, got.ImportanceScore, 1e-9)
	assert.Equal(t, []float32{0.5, -0.5, 0.25}, got.Embedding)
	assert.Equal(t, now, got.Timestamp)
	assert.Equal(t, "test", got.Source)
	assert.Zero(t, got.AccessCount)
	assert.True(t, got.LastAccessed.IsZero())
	assert.Zero(t, got.SupersededBy)
	assert.True(t, got.ValidTo.IsZero())
}

func TestStore_GetMissingReturnsErrNotFound(t *testing.T) {
	st, _ := openStore(t)
	_, err := st.Get(context.Background(), 42)
	require.ErrorIs(t, err, memory.ErrNotFound)
}

func TestStore_ActiveListsAll(t *testing.T) {
	st, _ := openStore(t)
	ctx := context.Background()
	for range 3 {
		_, err := st.Insert(ctx, sample("entry"))
		require.NoError(t, err)
	}
	active, err := st.Active(ctx)
	require.NoError(t, err)
	assert.Len(t, active, 3)
}

func TestStore_ArchiveMovesRowAndRecordsChain(t *testing.T) {
	st, path := openStore(t)
	ctx := context.Background()

	oldID, err := st.Insert(ctx, sample("old version"))
	require.NoError(t, err)
	newID, err := st.Insert(ctx, sample("new version"))
	require.NoError(t, err)

	require.NoError(t, st.Archive(ctx, oldID, newID, now))

	_, err = st.Get(ctx, oldID)
	require.ErrorIs(t, err, memory.ErrNotFound)

	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer db.Close()
	var content string
	var supersededBy, validTo int64
	require.NoError(t, db.QueryRow(
		`SELECT content, superseded_by, valid_to FROM memories_archive WHERE id = ?`, oldID,
	).Scan(&content, &supersededBy, &validTo))
	assert.Equal(t, "old version", content)
	assert.Equal(t, newID, supersededBy)
	assert.Equal(t, now.Unix(), validTo)
}

func TestStore_ArchiveMissingReturnsErrNotFound(t *testing.T) {
	st, _ := openStore(t)
	err := st.Archive(context.Background(), 42, 0, now)
	require.ErrorIs(t, err, memory.ErrNotFound)
}

func TestStore_ArchivedRowsLeaveKeywordSearch(t *testing.T) {
	st, _ := openStore(t)
	ctx := context.Background()

	id, err := st.Insert(ctx, sample("unique zebra fact"))
	require.NoError(t, err)

	ranks, err := st.FTSRanks(ctx, "zebra")
	require.NoError(t, err)
	assert.Contains(t, ranks, id)

	require.NoError(t, st.Archive(ctx, id, 0, now))

	ranks, err = st.FTSRanks(ctx, "zebra")
	require.NoError(t, err)
	assert.Empty(t, ranks)
}

func TestStore_FTSRanksMatchesOnlyKeywordRows(t *testing.T) {
	st, _ := openStore(t)
	ctx := context.Background()

	cat, err := st.Insert(ctx, sample("the cat sat on the mat"))
	require.NoError(t, err)
	_, err = st.Insert(ctx, sample("quantum entanglement notes"))
	require.NoError(t, err)

	ranks, err := st.FTSRanks(ctx, "cat")
	require.NoError(t, err)
	require.Len(t, ranks, 1)
	assert.Contains(t, ranks, cat)
}

func TestStore_FTSRanksIgnoresSpecialCharacters(t *testing.T) {
	st, _ := openStore(t)
	ctx := context.Background()
	id, err := st.Insert(ctx, sample("release plan"))
	require.NoError(t, err)

	for _, query := range []string{`"release`, `release OR`, `release*`, `NEAR(release)`} {
		_, err := st.FTSRanks(ctx, query)
		assert.NoError(t, err, "query %q must not inject FTS syntax", query)
	}

	ranks, err := st.FTSRanks(ctx, `release OR`)
	require.NoError(t, err)
	assert.Contains(t, ranks, id)
}

func TestStore_RecordAccess(t *testing.T) {
	st, _ := openStore(t)
	ctx := context.Background()

	id, err := st.Insert(ctx, sample("fact"))
	require.NoError(t, err)

	require.NoError(t, st.RecordAccess(ctx, []int64{id}, now))
	require.NoError(t, st.RecordAccess(ctx, []int64{id}, now.Add(time.Hour)))

	got, err := st.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, int64(2), got.AccessCount)
	assert.Equal(t, now.Add(time.Hour), got.LastAccessed)
}

func TestStore_ReopenKeepsData(t *testing.T) {
	st, path := openStore(t)
	ctx := context.Background()
	id, err := st.Insert(ctx, sample("persistent fact"))
	require.NoError(t, err)
	require.NoError(t, st.Close())

	reopened, err := sqlite.Open(path)
	require.NoError(t, err)
	defer reopened.Close()
	got, err := reopened.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "persistent fact", got.Content)
}
