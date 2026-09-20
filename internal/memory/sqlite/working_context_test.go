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
	work.Account = memory.WorkAccount{Status: memory.WorkActive, Goal: "prepare release", Constraints: "owner approval", Decisions: "use signed build", Progress: "build verified", Remaining: "await approval", Uncertainty: "signing time unknown"}
	work.Inputs = []string{"keep the original supplier"}
	work.Attempts = []memory.WorkAttempt{{Name: "publish", Input: json.RawMessage(`{"path":"release"}`)}}
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
	require.ErrorIs(t, err, memory.ErrStaleWorkingContext)
	after, err := st.LoadWorkingContext(ctx)
	require.NoError(t, err)
	assert.Equal(t, work, after)
}

func TestWorkingContext_MigratesWithoutLosingFacts(t *testing.T) {
	st, path := openStore(t)
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
	work, err := migrated.LoadWorkingContext(context.Background())
	require.NoError(t, err)
	assert.Equal(t, memory.WorkIdle, work.Account.Status)
}

func TestWorkingContext_RejectsInvalidStoredState(t *testing.T) {
	for _, query := range []string{
		`UPDATE working_context SET version = 2`,
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
