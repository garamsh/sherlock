package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
)

func TestWorkingContext_RoundTripAndStaleWriter(t *testing.T) {
	st, path := openStore(t)
	ctx := context.Background()
	initial, err := st.LoadWorkingContext(ctx)
	require.NoError(t, err)
	assert.Equal(t, memory.WorkIdle, initial.Account.Status)
	work := initial
	work.Account = memory.WorkAccount{Status: memory.WorkActive, Progress: "build verified; await approval; signing time unknown"}
	work.Source = []string{"prepare release with owner approval"}
	arrival := "keep the original supplier"
	work.Arrival = &arrival
	work.Attempts = []memory.WorkAttempt{{ID: 1, Name: "publish", Input: json.RawMessage(`{"path":"release"}`)}}
	revision, err := st.SaveWorkingContext(ctx, work)
	require.NoError(t, err)
	work.Revision = revision
	reopened, err := sqlite.Open(path)
	require.NoError(t, err)
	defer reopened.Close()
	restored, err := reopened.LoadWorkingContext(ctx)
	require.NoError(t, err)
	assert.Equal(t, work, restored)
	_, err = st.SaveWorkingContext(ctx, initial)
	require.ErrorContains(t, err, "stale working context")
	after, err := st.LoadWorkingContext(ctx)
	require.NoError(t, err)
	assert.Equal(t, work, after)
}

func TestWorkingContext_MigratesWithoutLosingFacts(t *testing.T) {
	st, path := openStore(t)
	archivedID := writeOne(t, st, sample(project, "archived fact"))
	require.NoError(t, st.Archive(context.Background(), archivedID, 0, now))
	writeOne(t, st, sample(project, "original fact"))
	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer raw.Close()
	_, err = raw.Exec(`DROP TABLE working_context`)
	require.NoError(t, err)
	var count int
	require.NoError(t, raw.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'working_context'`).Scan(&count))
	require.Zero(t, count, "fixture has the addressed schema before current-work support")
	migrated, err := sqlite.Open(path)
	require.NoError(t, err)
	defer migrated.Close()
	assert.Equal(t, "original fact", only(t, mustAt(t, migrated, project)).Value)
	var archived string
	require.NoError(t, raw.QueryRow(`SELECT value FROM memories_archive WHERE id = ?`, archivedID).Scan(&archived))
	assert.Equal(t, "archived fact", archived)
	work, err := migrated.LoadWorkingContext(context.Background())
	require.NoError(t, err)
	assert.Equal(t, memory.WorkIdle, work.Account.Status)
}

func TestWorkingContext_RejectsInvalidStoredState(t *testing.T) {
	for _, query := range []string{
		`UPDATE working_context SET version = 1`,
		`UPDATE working_context SET revision = -1`,
		`UPDATE working_context SET document = '{"account":{"status":"active"}}'`,
		`UPDATE working_context SET document = 'broken'`,
		`DELETE FROM working_context`,
	} {
		t.Run(query, func(t *testing.T) {
			st, path := openStore(t)
			_, err := st.LoadWorkingContext(context.Background())
			require.NoError(t, err, "accepted control before corruption")
			raw, err := sql.Open("sqlite", path)
			require.NoError(t, err)
			defer raw.Close()
			result, err := raw.Exec(query)
			require.NoError(t, err)
			changed, err := result.RowsAffected()
			require.NoError(t, err)
			require.EqualValues(t, 1, changed)
			_, err = st.LoadWorkingContext(context.Background())
			require.Error(t, err)
			reopened, err := sqlite.Open(path)
			if reopened != nil {
				reopened.Close()
			}
			require.Error(t, err, "startup must expose corruption")
		})
	}
}

func TestWorkingContext_WriteFailure(t *testing.T) {
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "memory.db"))
	require.NoError(t, err)
	work, err := st.LoadWorkingContext(context.Background())
	require.NoError(t, err)
	_, err = st.SaveWorkingContext(context.Background(), work)
	require.NoError(t, err, "accepted write before the connection closes")
	require.NoError(t, st.Close())
	_, err = st.SaveWorkingContext(context.Background(), work)
	require.Error(t, err)
}

func TestWorkingContext_RejectsExperimentalSummaryWithoutInventingSource(t *testing.T) {
	st, path := openStore(t)
	id := writeOne(t, st, sample(project, "archived fact"))
	require.NoError(t, st.Archive(context.Background(), id, 0, now))
	writeOne(t, st, sample(project, "active fact"))
	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer raw.Close()
	original := `{"account":{"status":"active","goal":"summary with missing clauses","constraints":"","decisions":"","progress":"waiting","remaining":"release","uncertainty":""},"inputs":[],"attempts":[]}`
	_, err = raw.Exec(`UPDATE working_context SET version = 1, revision = 8, document = ?`, original)
	require.NoError(t, err)
	reopened, err := sqlite.Open(path)
	require.ErrorContains(t, err, "working context version")
	require.Nil(t, reopened)
	var document, active, archived string
	var revision, version int
	require.NoError(t, raw.QueryRow(`SELECT version, revision, document FROM working_context WHERE id = 1`).Scan(&version, &revision, &document))
	assert.Equal(t, 1, version)
	assert.Equal(t, 8, revision)
	assert.Equal(t, original, document, "refusal preserves measured branch-only document")
	require.NoError(t, raw.QueryRow(`SELECT value FROM memories_active`).Scan(&active))
	assert.Equal(t, "active fact", active)
	require.NoError(t, raw.QueryRow(`SELECT value FROM memories_archive`).Scan(&archived))
	assert.Equal(t, "archived fact", archived)
}

func TestWorkingContext_RejectsOptionalSourceWithoutRewritingEvidence(t *testing.T) {
	st, path := openStore(t)
	writeOne(t, st, sample(project, "retained fact"))
	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer raw.Close()
	original := `{"account":{"status":"idle","progress":"waiting after omitted task"},"source":[],"inputs":[],"attempts":[]}`
	_, err = raw.Exec(`UPDATE working_context SET version = 2, revision = 9, document = ?`, original)
	require.NoError(t, err)
	reopened, err := sqlite.Open(path)
	require.ErrorContains(t, err, "working context version")
	require.Nil(t, reopened)
	var document, fact string
	var version, revision int
	require.NoError(t, raw.QueryRow(`SELECT version, revision, document FROM working_context`).Scan(&version, &revision, &document))
	assert.Equal(t, 2, version)
	assert.Equal(t, 9, revision)
	assert.Equal(t, original, document)
	require.NoError(t, raw.QueryRow(`SELECT value FROM memories_active`).Scan(&fact))
	assert.Equal(t, "retained fact", fact)
}

func TestWorkingContext_RejectsIndexedInputWithoutRewritingEvidence(t *testing.T) {
	st, path := openStore(t)
	writeOne(t, st, sample(project, "retained fact"))
	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer raw.Close()
	original := `{"account":{"status":"canceled","progress":"canceled before neutral arrival"},"source":["original exact instruction"],"inputs":[],"attempts":[]}`
	_, err = raw.Exec(`UPDATE working_context SET version = 3, revision = 14, document = ?`, original)
	require.NoError(t, err)
	reopened, err := sqlite.Open(path)
	require.ErrorContains(t, err, "working context version")
	require.Nil(t, reopened)
	var document, fact string
	var version, revision int
	require.NoError(t, raw.QueryRow(`SELECT version, revision, document FROM working_context`).Scan(&version, &revision, &document))
	assert.Equal(t, 3, version)
	assert.Equal(t, 14, revision)
	assert.Equal(t, original, document)
	require.NoError(t, raw.QueryRow(`SELECT value FROM memories_active`).Scan(&fact))
	assert.Equal(t, "retained fact", fact)
}
