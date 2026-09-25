package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

// firstSummary isolates persistence and ranking from model quality.
type firstSummary struct{}

func (firstSummary) Summarize(_ context.Context, updates []string) (string, error) {
	return updates[0], nil
}

type summaryFunc func(context.Context, []string) (string, error)

func (f summaryFunc) Summarize(ctx context.Context, updates []string) (string, error) {
	return f(ctx, updates)
}

func episodeSources(content string) []agent.SourceMessage {
	var sources []agent.SourceMessage
	for i := 0; i < 8; i++ {
		sources = append(sources, agent.SourceMessage{Role: agent.RoleUser, Content: content})
	}
	return sources
}

func TestConversation_CredentialsAreNotPersisted(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "memory.db"))
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: "api_key=sk-abcdefghijklmnop and favorite tea is cedar"}, {Role: agent.RoleAssistant, Content: "Bearer secretvalue"}}, firstSummary{}))
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

func TestOpenConversation_ExtractiveDatabaseRequiresExplicitNewPath(t *testing.T) {
	for _, populated := range []bool{false, true} {
		t.Run(fmt.Sprint(populated), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "old.db")
			raw, err := sql.Open("sqlite", path)
			require.NoError(t, err)
			_, err = raw.Exec(`CREATE TABLE conversation_sources(id INTEGER PRIMARY KEY, role TEXT, external_id TEXT, content TEXT); CREATE TABLE conversation_short(id INTEGER PRIMARY KEY,source_id INTEGER,summary TEXT); CREATE TABLE conversation_mid(id INTEGER PRIMARY KEY,short_id INTEGER,summary TEXT)`)
			require.NoError(t, err)
			if populated {
				_, err = raw.Exec(`INSERT INTO conversation_sources VALUES(1,'user','','original'); INSERT INTO conversation_short VALUES(1,1,'original'); INSERT INTO conversation_mid VALUES(1,1,'original')`)
				require.NoError(t, err)
			}
			require.NoError(t, raw.Close())
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			store, err := sqlite.OpenConversation(path)
			if populated {
				require.ErrorContains(t, err, "new memory-path")
				after, err := os.ReadFile(path)
				require.NoError(t, err)
				assert.Equal(t, before, after)
				store, err = sqlite.OpenConversation(filepath.Join(t.TempDir(), "new.db"))
				require.NoError(t, err)
			} else {
				require.NoError(t, err)
			}
			defer store.Close()
			require.NoError(t, store.RecordTurn(context.Background(), []agent.SourceMessage{{Role: agent.RoleUser, Content: "new conversation"}}, firstSummary{}))
		})
	}
}

func TestRecordTurn_CrossTurnEpisodesRestartAndRetry(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.OpenConversation(path)
	require.NoError(t, err)
	calls := 0
	summarize := summaryFunc(func(_ context.Context, updates []string) (string, error) {
		calls++
		require.Len(t, updates, 8)
		return updates[0], nil
	})
	for i := 0; i < 3; i++ {
		require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, ExternalID: fmt.Sprintf("input-%d", i), Content: fmt.Sprintf("fact-%d", i)}, {Role: agent.RoleAssistant, Content: "Noted."}}, summarize))
	}
	assert.Zero(t, calls)
	pending, err := store.PendingSources(ctx)
	require.NoError(t, err)
	require.Len(t, pending, 6)
	require.NoError(t, store.Close())
	store, err = sqlite.OpenConversation(path)
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.Accumulate(ctx, summarize))
	assert.Zero(t, calls)
	turn := []agent.SourceMessage{{Role: agent.RoleUser, ExternalID: "last", Content: "fact-3"}, {Role: agent.RoleAssistant, Content: "Noted."}}
	require.NoError(t, store.RecordTurn(ctx, turn, summarize))
	assert.Equal(t, 1, calls)
	require.NoError(t, store.RecordTurn(ctx, turn, summarize))
	assert.Equal(t, 1, calls)
	mids, err := store.RecentMid(ctx, 10)
	require.NoError(t, err)
	require.Len(t, mids, 1)
	assert.EqualValues(t, 1, mids[0].SourceFirst)
	assert.EqualValues(t, 8, mids[0].SourceLast)
	for id := mids[0].SourceFirst; id <= mids[0].SourceLast; id++ {
		source, err := store.Source(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, id, source.ID)
	}
	pending, err = store.PendingSources(ctx)
	require.NoError(t, err)
	assert.Empty(t, pending)
	require.NoError(t, store.RecordTurn(ctx, episodeSources("Another episode"), summarize))
	assert.Equal(t, 2, calls)
	mids, err = store.RecentMid(ctx, 10)
	require.NoError(t, err)
	require.Len(t, mids, 2)
	assert.EqualValues(t, 9, mids[0].SourceFirst)
	assert.EqualValues(t, 16, mids[0].SourceLast)
	assert.NotContains(t, mids[0].Summary, "fact-0", "an episode receives only its own source batch")
	turn[0].Content = "conflicting reuse"
	require.ErrorContains(t, store.RecordTurn(ctx, turn, summarize), "different content")
}

func TestRecordTurn_FailedSynthesisLeavesRetryableSources(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.OpenConversation(path)
	require.NoError(t, err)
	require.NoError(t, store.RecordTurn(ctx, episodeSources("Old episode"), firstSummary{}))
	update := episodeSources("Correction: current codename is Silver Pine")
	for i := range update {
		update[i].ExternalID = fmt.Sprintf("retry-%d", i)
	}
	failed := summaryFunc(func(context.Context, []string) (string, error) { return "", errors.New("interrupted model") })
	require.ErrorContains(t, store.RecordTurn(ctx, update, failed), "interrupted model")
	require.NoError(t, store.Close())
	store, err = sqlite.OpenConversation(path)
	require.NoError(t, err)
	defer store.Close()
	sources, err := store.Recent(ctx, 100)
	require.NoError(t, err)
	require.Len(t, sources, 16)
	pending, err := store.PendingSources(ctx)
	require.NoError(t, err)
	require.Len(t, pending, 8)
	assert.EqualValues(t, 9, pending[0].ID)
	mids, err := store.RecentMid(ctx, 100)
	require.NoError(t, err)
	require.Len(t, mids, 1)
	require.NoError(t, store.RecordTurn(ctx, update, firstSummary{}))
	mids, err = store.RecentMid(ctx, 100)
	require.NoError(t, err)
	require.Len(t, mids, 2)
	assert.Contains(t, mids[0].Summary, "Silver Pine")
	assert.EqualValues(t, 9, mids[0].SourceFirst)
	sources, err = store.Recent(ctx, 100)
	require.NoError(t, err)
	require.Len(t, sources, 16)
}

func TestSearch_RelevanceThenRecencyAndUnsupportedControl(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenConversation(filepath.Join(t.TempDir(), "memory.db"))
	require.NoError(t, err)
	defer store.Close()
	for _, content := range []string{"Correction: current project codename is Silver Pine; Blue Cedar is obsolete.", "Routine project status", "Routine project status", "Routine project status", "Routine project status", "Routine project status", "Routine project status"} {
		require.NoError(t, store.RecordTurn(ctx, episodeSources(content), firstSummary{}))
	}
	hits, err := store.Search(ctx, []string{"current project codename"}, 5)
	require.NoError(t, err)
	require.Len(t, hits, 5)
	assert.EqualValues(t, 1, hits[0].ID)
	hits, err = store.Search(ctx, []string{"Routine project status"}, 2)
	require.NoError(t, err)
	require.Len(t, hits, 2)
	assert.EqualValues(t, 7, hits[0].ID)
	assert.EqualValues(t, 6, hits[1].ID)
	for _, terms := range [][]string{{"violet submarine"}, {"the and ?"}} {
		hits, err = store.Search(ctx, terms, 5)
		require.NoError(t, err)
		assert.Empty(t, hits)
	}
	hits, err = store.Search(ctx, []string{"project"}, 0)
	require.NoError(t, err)
	assert.Empty(t, hits)
}
