package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
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

// wordEmbedder counts each word into a hashed dimension, so texts sharing
// words are similar and texts sharing none are orthogonal.
type wordEmbedder struct{}

func (wordEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	vector := make([]float32, 4096)
	for _, word := range strings.Fields(strings.ToLower(text)) {
		hash := fnv.New32a()
		_, _ = hash.Write([]byte(strings.Trim(word, ".,;:?!")))
		vector[hash.Sum32()%4096]++
	}
	return vector, nil
}

// episodeSources returns one full batch of identical messages.
func episodeSources(content string) []agent.SourceMessage {
	var sources []agent.SourceMessage
	for i := 0; i < 4; i++ {
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
	_, err = sqlite.OpenConversation(path, wordEmbedder{})
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
			store, err := sqlite.OpenConversation(path, wordEmbedder{})
			if populated {
				require.ErrorContains(t, err, "new memory-path")
				after, err := os.ReadFile(path)
				require.NoError(t, err)
				assert.Equal(t, before, after)
				store, err = sqlite.OpenConversation(filepath.Join(t.TempDir(), "new.db"), wordEmbedder{})
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
	store, err := sqlite.OpenConversation(path, wordEmbedder{})
	require.NoError(t, err)
	calls := 0
	summarize := summaryFunc(func(_ context.Context, updates []string) (string, error) {
		calls++
		require.Len(t, updates, 4)
		return updates[0], nil
	})
	for i := 0; i < 1; i++ {
		require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, ExternalID: fmt.Sprintf("input-%d", i), Content: fmt.Sprintf("fact-%d", i)}, {Role: agent.RoleAssistant, Content: "Noted."}}, summarize))
	}
	assert.Zero(t, calls)
	pending, err := store.PendingSources(ctx)
	require.NoError(t, err)
	require.Len(t, pending, 2)
	require.NoError(t, store.Close())
	store, err = sqlite.OpenConversation(path, wordEmbedder{})
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
	assert.EqualValues(t, 4, mids[0].SourceLast)
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
	assert.EqualValues(t, 5, mids[0].SourceFirst)
	assert.EqualValues(t, 8, mids[0].SourceLast)
	assert.NotContains(t, mids[0].Summary, "fact-0", "an episode receives only its own source batch")
	turn[0].Content = "conflicting reuse"
	require.ErrorContains(t, store.RecordTurn(ctx, turn, summarize), "different content")
}

func TestRecordTurn_FailedSynthesisLeavesRetryableSources(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.OpenConversation(path, wordEmbedder{})
	require.NoError(t, err)
	require.NoError(t, store.RecordTurn(ctx, episodeSources("Old episode"), firstSummary{}))
	update := episodeSources("Correction: current codename is Silver Pine")
	for i := range update {
		update[i].ExternalID = fmt.Sprintf("retry-%d", i)
	}
	failed := summaryFunc(func(context.Context, []string) (string, error) { return "", errors.New("interrupted model") })
	require.ErrorContains(t, store.RecordTurn(ctx, update, failed), "interrupted model")
	require.NoError(t, store.Close())
	store, err = sqlite.OpenConversation(path, wordEmbedder{})
	require.NoError(t, err)
	defer store.Close()
	sources, err := store.Recent(ctx, 100)
	require.NoError(t, err)
	require.Len(t, sources, 8)
	pending, err := store.PendingSources(ctx)
	require.NoError(t, err)
	require.Len(t, pending, 4)
	assert.EqualValues(t, 5, pending[0].ID)
	mids, err := store.RecentMid(ctx, 100)
	require.NoError(t, err)
	require.Len(t, mids, 1)
	require.NoError(t, store.RecordTurn(ctx, update, firstSummary{}))
	mids, err = store.RecentMid(ctx, 100)
	require.NoError(t, err)
	require.Len(t, mids, 2)
	assert.Contains(t, mids[0].Summary, "Silver Pine")
	assert.EqualValues(t, 5, mids[0].SourceFirst)
	sources, err = store.Recent(ctx, 100)
	require.NoError(t, err)
	require.Len(t, sources, 8)
}

func TestSearch_SimilarityThenRecency(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenConversation(filepath.Join(t.TempDir(), "memory.db"), wordEmbedder{})
	require.NoError(t, err)
	defer store.Close()
	for _, content := range []string{"Correction: current project codename is Silver Pine; Blue Cedar is obsolete.", "Routine project status", "Routine project status", "Routine project status", "Routine project status", "Routine project status", "Routine project status"} {
		require.NoError(t, store.RecordTurn(ctx, episodeSources(content), firstSummary{}))
	}
	hits, err := store.Search(ctx, "current project codename", 5)
	require.NoError(t, err)
	require.Len(t, hits, 5)
	assert.EqualValues(t, 1, hits[0].ID)
	hits, err = store.Search(ctx, "Routine project status", 2)
	require.NoError(t, err)
	require.Len(t, hits, 2)
	assert.EqualValues(t, 7, hits[0].ID)
	assert.EqualValues(t, 6, hits[1].ID)
	// No threshold: a query sharing no word ties every episode, newest first.
	hits, err = store.Search(ctx, "violet submarine", 3)
	require.NoError(t, err)
	assert.Equal(t, []int64{7, 6, 5}, hitIDs(hits))
	hits, err = store.Search(ctx, "project", 0)
	require.NoError(t, err)
	assert.Empty(t, hits)
}

func hitIDs(hits []agent.ConversationHit) []int64 {
	var ids []int64
	for _, hit := range hits {
		ids = append(ids, hit.ID)
	}
	return ids
}

// failingEmbedder refuses every text, as an unreachable endpoint would.
type failingEmbedder struct{}

func (failingEmbedder) Embed(context.Context, string) ([]float32, error) {
	return nil, errors.New("embedding endpoint unavailable")
}

func TestAccumulate_EmbeddingFailureRetriesAtNextCompletion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.OpenConversation(path, failingEmbedder{})
	require.NoError(t, err)
	require.ErrorContains(t, store.RecordTurn(ctx, episodeSources("Blue Cedar launch"), firstSummary{}), "embedding endpoint unavailable")
	mids, err := store.RecentMid(ctx, 10)
	require.NoError(t, err)
	require.Len(t, mids, 1, "the episode and its checkpoint commit before embedding")
	require.NoError(t, store.Close())

	store, err = sqlite.OpenConversation(path, wordEmbedder{})
	require.NoError(t, err)
	defer store.Close()
	hits, err := store.Search(ctx, "Blue Cedar", 5)
	require.NoError(t, err)
	assert.Empty(t, hits, "an episode without an embedding is not ranked")
	require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: "next"}}, firstSummary{}))
	hits, err = store.Search(ctx, "Blue Cedar", 5)
	require.NoError(t, err)
	assert.Equal(t, []int64{mids[0].ID}, hitIDs(hits))
}

func TestSearch_RefusesEmbeddingsOfAnotherModel(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.OpenConversation(path, wordEmbedder{})
	require.NoError(t, err)
	require.NoError(t, store.RecordTurn(ctx, episodeSources("Blue Cedar launch"), firstSummary{}))
	require.NoError(t, store.Close())
	store, err = sqlite.OpenConversation(path, constantEmbedder{})
	require.NoError(t, err)
	defer store.Close()
	_, err = store.Search(ctx, "Blue Cedar", 5)
	require.ErrorContains(t, err, "the embedding model changed")
}

// constantEmbedder returns a one-dimensional vector for every text.
type constantEmbedder struct{}

func (constantEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{1}, nil
}

// An existing file keeps its batch-8 episodes: startup drops the FTS5 index,
// adds the embedding column and embeds them, and they stay readable.
func TestOpenConversation_EmbedsEpisodesFromBeforeEmbeddings(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = raw.Exec(`CREATE TABLE conversation_sources (id INTEGER PRIMARY KEY AUTOINCREMENT, role TEXT NOT NULL, external_id TEXT, content TEXT NOT NULL);
CREATE TABLE conversation_mid (id INTEGER PRIMARY KEY AUTOINCREMENT, source_first INTEGER NOT NULL UNIQUE, source_last INTEGER NOT NULL UNIQUE, summary TEXT NOT NULL);
CREATE VIRTUAL TABLE conversation_mid_fts USING fts5(summary, content='conversation_mid', content_rowid='id');
CREATE TRIGGER conversation_mid_insert AFTER INSERT ON conversation_mid BEGIN INSERT INTO conversation_mid_fts(rowid, summary) VALUES (new.id, new.summary); END;`)
	require.NoError(t, err)
	for i := 1; i <= 8; i++ {
		_, err = raw.Exec(`INSERT INTO conversation_sources(role,external_id,content) VALUES ('user','',?)`, fmt.Sprintf("old-%d", i))
		require.NoError(t, err)
	}
	_, err = raw.Exec(`INSERT INTO conversation_mid(source_first,source_last,summary) VALUES (1,8,'The telescope is a Vega reflector.')`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	store, err := sqlite.OpenConversation(path, wordEmbedder{})
	require.NoError(t, err)
	defer store.Close()
	hits, err := store.Search(ctx, "Vega telescope", 5)
	require.NoError(t, err)
	assert.Empty(t, hits, "opening alone embeds nothing")
	require.NoError(t, store.Accumulate(ctx, firstSummary{}))
	hits, err = store.Search(ctx, "Vega telescope", 5)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.EqualValues(t, 1, hits[0].SourceFirst)
	assert.EqualValues(t, 8, hits[0].SourceLast)
	source, err := store.Source(ctx, hits[0].SourceLast)
	require.NoError(t, err)
	assert.Equal(t, "old-8", source.Content)

	raw, err = sql.Open("sqlite", path)
	require.NoError(t, err)
	defer raw.Close()
	var fts int
	require.NoError(t, raw.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name IN ('conversation_mid_fts','conversation_mid_insert')`).Scan(&fts))
	assert.Zero(t, fts)
}

func TestNote_EmptyUntilSetThenReplacedAndReopened(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.OpenConversation(path, wordEmbedder{})
	require.NoError(t, err)
	note, err := store.Note(ctx)
	require.NoError(t, err)
	assert.Empty(t, note, "a store no note was written to has an empty note")

	require.NoError(t, store.SetNote(ctx, "Indent code with tabs."))
	require.NoError(t, store.SetNote(ctx, "Indent code with tabs. Current work: the Oslo packing list."))
	require.NoError(t, store.Close())

	reopened, err := sqlite.OpenConversation(path, wordEmbedder{})
	require.NoError(t, err)
	defer reopened.Close()
	note, err = reopened.Note(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Indent code with tabs. Current work: the Oslo packing list.", note, "the latest note replaces the earlier one and survives a restart")
}

func TestNote_CredentialsAreNotPersisted(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenConversation(filepath.Join(t.TempDir(), "memory.db"), wordEmbedder{})
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.SetNote(ctx, "Deploy with password: hunter22 to the staging host."))
	note, err := store.Note(ctx)
	require.NoError(t, err)
	assert.NotContains(t, note, "hunter22")
	assert.Contains(t, note, "staging host", "the rest of the note is kept")
}

// TestNote_DatabaseFromBeforeTheNoteOpensWithAnEmptyNote opens a file
// written before the note existed: its conversation survives, the note
// starts empty, and it can be written.
func TestNote_DatabaseFromBeforeTheNoteOpensWithAnEmptyNote(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.OpenConversation(path, wordEmbedder{})
	require.NoError(t, err)
	require.NoError(t, store.RecordTurn(ctx, episodeSources("The telescope is the Kepler-9 refractor."), firstSummary{}))
	require.NoError(t, store.Close())
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = db.Exec(`DROP TABLE conversation_note`)
	require.NoError(t, err)
	var tables int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='conversation_note'`).Scan(&tables))
	require.Zero(t, tables, "the setup produced a file without the note table")
	require.NoError(t, db.Close())

	reopened, err := sqlite.OpenConversation(path, wordEmbedder{})
	require.NoError(t, err)
	defer reopened.Close()
	note, err := reopened.Note(ctx)
	require.NoError(t, err)
	assert.Empty(t, note)
	sources, err := reopened.Recent(ctx, 10)
	require.NoError(t, err)
	assert.NotEmpty(t, sources, "the conversation from before the note is kept")
	require.NoError(t, reopened.SetNote(ctx, "Indent code with tabs."))
	note, err = reopened.Note(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Indent code with tabs.", note)
}
