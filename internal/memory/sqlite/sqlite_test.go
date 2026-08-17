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

// project is the address most of these tests write to.
var project = memory.Address{Subject: "Alma", Question: "which project does she lead"}

func openStore(t *testing.T) (*sqlite.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memory.db")
	st, err := sqlite.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, st.Close()) })
	return st, path
}

func sample(a memory.Address, value string) memory.Memory {
	return memory.Memory{
		Address:         a,
		Value:           value,
		ImportanceScore: 0.7,
		Timestamp:       now,
		Source:          "test",
	}
}

// mustAt reads one address, and only returns the single memory a read
// was expected to reach.
func mustAt(t *testing.T, st *sqlite.Store, a memory.Address) []memory.Memory {
	t.Helper()
	held, err := st.At(context.Background(), a)
	require.NoError(t, err)
	return held
}

func only(t *testing.T, held []memory.Memory) memory.Memory {
	t.Helper()
	require.Len(t, held, 1)
	return held[0]
}

func TestStore_InsertRoundTrip(t *testing.T) {
	st, _ := openStore(t)
	ctx := context.Background()

	id, err := st.Insert(ctx, sample(project, "amber-quarry"))
	require.NoError(t, err)
	require.Positive(t, id)

	got := only(t, mustAt(t, st, project))
	assert.Equal(t, id, got.ID)
	assert.Equal(t, project, got.Address)
	assert.Equal(t, "amber-quarry", got.Value)
	assert.InDelta(t, 0.7, got.ImportanceScore, 1e-9)
	assert.Equal(t, now, got.Timestamp)
	assert.Equal(t, "test", got.Source)
	assert.Zero(t, got.AccessCount)
	assert.Zero(t, got.SupersededBy)
}

func TestStore_AtIsEmptyForAnAddressNothingWasWrittenTo(t *testing.T) {
	st, _ := openStore(t)
	held, err := st.At(context.Background(), project)
	require.NoError(t, err)
	assert.Empty(t, held)
}

// The three reads are the three prefixes of one address: the address
// itself, the question it asks, and the subject it is about.
func TestStore_TheThreeReadsAreThreePrefixesOfOneAddress(t *testing.T) {
	st, _ := openStore(t)
	ctx := context.Background()
	onCall := project
	onCall.Restriction = "on call"
	team := memory.Address{Subject: "Alma", Question: "which team does she sit on"}

	for _, m := range []memory.Memory{
		sample(project, "amber-quarry"),
		sample(onCall, "basalt-run"),
		sample(team, "platform"),
		sample(memory.Address{Subject: "Otto", Question: "which project does he lead"}, "cinder-lake"),
	} {
		_, err := st.Insert(ctx, m)
		require.NoError(t, err)
	}

	exact, err := st.At(ctx, project)
	require.NoError(t, err)
	assert.Equal(t, []string{"amber-quarry"}, valuesOf(exact), "the address itself")

	question, err := st.UnderQuestion(ctx, project.Subject, project.Question)
	require.NoError(t, err)
	assert.Equal(t, []string{"amber-quarry", "basalt-run"}, valuesOf(question),
		"the question asked and the restrictions under it, unrestricted first")

	subject, err := st.UnderSubject(ctx, "Alma")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"amber-quarry", "basalt-run", "platform"}, valuesOf(subject),
		"every address under the subject, and no other subject's")
}

func valuesOf(held []memory.Memory) []string {
	out := make([]string, 0, len(held))
	for _, m := range held {
		out = append(out, m.Value)
	}
	return out
}

// One value at one address is held once. The second write moves the
// timestamp and returns the row that was already there, so a restated
// fact does not accumulate rows.
func TestStore_InsertingAValueTwiceAtOneAddressHoldsItOnce(t *testing.T) {
	st, _ := openStore(t)
	ctx := context.Background()

	first, err := st.Insert(ctx, sample(project, "amber-quarry"))
	require.NoError(t, err)
	restated := sample(project, "amber-quarry")
	restated.Timestamp = now.Add(time.Hour)
	again, err := st.Insert(ctx, restated)
	require.NoError(t, err)

	assert.Equal(t, first, again, "the row that was already there comes back")
	got := only(t, mustAt(t, st, project))
	assert.Equal(t, now.Add(time.Hour), got.Timestamp, "the write moves the timestamp")
}

// The same value under a different question is a different fact and is
// held separately: the uniqueness is per address, not per value.
func TestStore_OneValueUnderTwoQuestionsIsTwoRows(t *testing.T) {
	st, _ := openStore(t)
	ctx := context.Background()
	team := memory.Address{Subject: "Alma", Question: "which team does she sit on"}

	_, err := st.Insert(ctx, sample(project, "platform"))
	require.NoError(t, err)
	_, err = st.Insert(ctx, sample(team, "platform"))
	require.NoError(t, err)

	active, err := st.Active(ctx)
	require.NoError(t, err)
	assert.Len(t, active, 2)
}

func TestStore_ArchiveMovesRowAndRecordsChain(t *testing.T) {
	st, path := openStore(t)
	ctx := context.Background()

	oldID, err := st.Insert(ctx, sample(project, "amber-quarry"))
	require.NoError(t, err)
	newID, err := st.Insert(ctx, sample(project, "basalt-run"))
	require.NoError(t, err)

	require.NoError(t, st.Archive(ctx, oldID, newID, now))

	held, err := st.At(ctx, project)
	require.NoError(t, err)
	assert.Equal(t, []string{"basalt-run"}, valuesOf(held), "an archived row leaves the read")

	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer db.Close()
	var value string
	var supersededBy, validTo int64
	require.NoError(t, db.QueryRow(
		`SELECT value, superseded_by, valid_to FROM memories_archive WHERE id = ?`, oldID,
	).Scan(&value, &supersededBy, &validTo))
	assert.Equal(t, "amber-quarry", value)
	assert.Equal(t, newID, supersededBy)
	assert.Equal(t, now.Unix(), validTo)
}

func TestStore_ArchiveMissingReturnsErrNotFound(t *testing.T) {
	st, _ := openStore(t)
	err := st.Archive(context.Background(), 42, 0, now)
	require.ErrorIs(t, err, memory.ErrNotFound)
}

// Archiving frees the address for the same value again: the uniqueness
// index covers the active table only, so a value can be written, retired
// and written again without colliding with its own archived row.
func TestStore_AnArchivedValueCanBeWrittenAgain(t *testing.T) {
	st, _ := openStore(t)
	ctx := context.Background()

	id, err := st.Insert(ctx, sample(project, "amber-quarry"))
	require.NoError(t, err)
	require.NoError(t, st.Archive(ctx, id, 0, now))

	again, err := st.Insert(ctx, sample(project, "amber-quarry"))
	require.NoError(t, err)
	assert.NotEqual(t, id, again)
	assert.Equal(t, []string{"amber-quarry"}, valuesOf(mustAt(t, st, project)))
}

func TestStore_RecordAccess(t *testing.T) {
	st, _ := openStore(t)
	ctx := context.Background()

	id, err := st.Insert(ctx, sample(project, "amber-quarry"))
	require.NoError(t, err)

	require.NoError(t, st.RecordAccess(ctx, []int64{id}, now))
	require.NoError(t, st.RecordAccess(ctx, []int64{id}, now.Add(time.Hour)))

	got := only(t, mustAt(t, st, project))
	assert.Equal(t, int64(2), got.AccessCount)
	assert.Equal(t, now.Add(time.Hour), got.LastAccessed)
}

func TestStore_ReopenKeepsData(t *testing.T) {
	st, path := openStore(t)
	ctx := context.Background()
	_, err := st.Insert(ctx, sample(project, "amber-quarry"))
	require.NoError(t, err)
	require.NoError(t, st.Close())

	reopened, err := sqlite.Open(path)
	require.NoError(t, err)
	defer reopened.Close()
	got := only(t, mustAt(t, reopened, project))
	assert.Equal(t, "amber-quarry", got.Value)
}

// A memory.db written before #582 holds sentences, and no address can be
// computed from one without a model call. Opening it reports that rather
// than failing on a missing column somewhere later.
func TestOpen_RefusesAPreAddressedStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE memories_active (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		content TEXT NOT NULL,
		embedding BLOB NOT NULL,
		type TEXT NOT NULL,
		importance_score REAL NOT NULL,
		access_count INTEGER NOT NULL DEFAULT 0,
		last_accessed INTEGER,
		timestamp INTEGER NOT NULL,
		source TEXT NOT NULL,
		superseded_by INTEGER,
		valid_to INTEGER)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = sqlite.Open(path)
	assert.ErrorIs(t, err, sqlite.ErrPreAddressedStore)
}
