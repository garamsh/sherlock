package sqlite_test

import (
	"context"
	"database/sql"
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
