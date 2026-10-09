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

func (firstSummary) Summarize(_ context.Context, updates []string) (agent.Synthesis, error) {
	return agent.Synthesis{Summary: updates[0]}, nil
}

type summaryFunc func(context.Context, []string) (string, error)

func (f summaryFunc) Summarize(ctx context.Context, updates []string) (agent.Synthesis, error) {
	summary, err := f(ctx, updates)
	return agent.Synthesis{Summary: summary}, err
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

// A message ID is unique within its sender, so two senders may commit the
// same ID, and each holds only its own.
func TestHoldsInbound_ScopesIDToSender(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenConversation(filepath.Join(t.TempDir(), "memory.db"), wordEmbedder{})
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Sender: "alice", ExternalID: "m1", Content: "from alice"}, {Role: agent.RoleAssistant, Content: "Noted."}}, firstSummary{}))
	require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Sender: "bob", ExternalID: "m1", Content: "from bob"}, {Role: agent.RoleAssistant, Content: "Noted."}}, firstSummary{}),
		"another sender's ID does not collide")

	held, same, err := store.HoldsInbound(ctx, "", "alice", "m1", "from alice")
	require.NoError(t, err)
	assert.True(t, held)
	assert.True(t, same, "the sender's own content is what it holds")
	held, same, err = store.HoldsInbound(ctx, "", "bob", "m1", "from alice")
	require.NoError(t, err)
	assert.True(t, held)
	assert.False(t, same, "bob's m1 holds bob's content, not alice's")
	held, _, err = store.HoldsInbound(ctx, "", "carol", "m1", "from alice")
	require.NoError(t, err)
	assert.False(t, held, "a sender that delivered nothing holds nothing")
}

// A replay carries the original content, credentials included; it matches
// the redacted copy the store holds.
func TestHoldsInbound_ComparesAsStored(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenConversation(filepath.Join(t.TempDir(), "memory.db"), wordEmbedder{})
	require.NoError(t, err)
	defer store.Close()
	content := "api_key=sk-abcdefghijklmnop and favorite tea is cedar"
	require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, ExternalID: "m1", Content: content}, {Role: agent.RoleAssistant, Content: "Noted."}}, firstSummary{}))
	source, err := store.Source(ctx, 1)
	require.NoError(t, err)
	require.NotEqual(t, content, source.Content, "the store holds the redacted copy")

	held, same, err := store.HoldsInbound(ctx, "", "", "m1", content)
	require.NoError(t, err)
	assert.True(t, held)
	assert.True(t, same)
	_, same, err = store.HoldsInbound(ctx, "", "", "m1", "favorite tea is oolong")
	require.NoError(t, err)
	assert.False(t, same, "other content does not match")
}

// A file from before sender scoping keeps its IDs under the empty sender,
// and another sender may then commit the same ID.
func TestOpenConversation_ScopesIDsFromBeforeSenders(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = raw.Exec(`CREATE TABLE conversation_sources (id INTEGER PRIMARY KEY AUTOINCREMENT, role TEXT NOT NULL CHECK(role IN ('user','assistant')), external_id TEXT, content TEXT NOT NULL);
CREATE UNIQUE INDEX conversation_external_id ON conversation_sources(external_id) WHERE external_id <> '';
INSERT INTO conversation_sources(role,external_id,content) VALUES ('user','msg-1','old message')`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	store, err := sqlite.OpenConversation(path, wordEmbedder{})
	require.NoError(t, err)
	defer store.Close()
	held, same, err := store.HoldsInbound(ctx, "", "", "msg-1", "old message")
	require.NoError(t, err)
	assert.True(t, held)
	assert.True(t, same)
	require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Sender: "alice", ExternalID: "msg-1", Content: "new message"}, {Role: agent.RoleAssistant, Content: "Noted."}}, firstSummary{}))
	held, same, err = store.HoldsInbound(ctx, "", "alice", "msg-1", "new message")
	require.NoError(t, err)
	assert.True(t, held)
	assert.True(t, same)
}

// A file from before signed senders (ADR 0026) keeps every row, which
// reads as a transport principal's: its scope and its poster are its
// sender. A signed source under the same sender and ID is another scope's,
// and answers its own poster.
func TestOpenConversation_KeepsRowsFromBeforeSignedSenders(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = raw.Exec(`CREATE TABLE conversation_sources (id INTEGER PRIMARY KEY AUTOINCREMENT, role TEXT NOT NULL CHECK(role IN ('user','assistant')), external_id TEXT, content TEXT NOT NULL, sender TEXT NOT NULL DEFAULT '');
CREATE UNIQUE INDEX conversation_sender_external_id ON conversation_sources(sender,external_id) WHERE external_id <> '';
INSERT INTO conversation_sources(role,sender,external_id,content) VALUES ('user','alice','msg-1','old message')`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	store, err := sqlite.OpenConversation(path, wordEmbedder{})
	require.NoError(t, err)
	defer store.Close()
	held, same, err := store.HoldsInbound(ctx, "", "alice", "msg-1", "old message")
	require.NoError(t, err)
	assert.True(t, held && same, "the old row is the transport principal's")
	posted, err := store.PostedInbound(ctx, "alice", "msg-1")
	require.NoError(t, err)
	assert.True(t, posted, "the old row's poster is its sender")
	posted, err = store.PostedInbound(ctx, "bob", "msg-1")
	require.NoError(t, err)
	assert.False(t, posted, "control: another poster does not hold it")

	require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{
		{Role: agent.RoleUser, Issuer: "https://issuer.example", Sender: "alice", Poster: "", ExternalID: "msg-1", Content: "signed message"},
		{Role: agent.RoleAssistant, Content: "Noted."},
	}, firstSummary{}), "the signed alice's msg-1 is not the transport alice's")
	held, same, err = store.HoldsInbound(ctx, "https://issuer.example", "alice", "msg-1", "signed message")
	require.NoError(t, err)
	assert.True(t, held && same)
	held, same, err = store.HoldsInbound(ctx, "", "alice", "msg-1", "old message")
	require.NoError(t, err)
	assert.True(t, held && same, "the old row is unchanged")
	posted, err = store.PostedInbound(ctx, "", "msg-1")
	require.NoError(t, err)
	assert.True(t, posted, "the signed message answers the principal that posted it")
}

// TestOpenInitializedConversation_RefusesAnUninitializedStore holds the
// opt-in startup refusal (ADR 0025): a memory.db that is absent, empty or
// holds no Sherlock schema is refused with ErrStoreUninitialized, and the
// refusal creates no store and changes no file.
//
// Each refusal carries its control on the same input: OpenConversation
// accepts it and initializes the store, so what refuses is the check and
// not a path no opener could use. The last case is the accepted one, an
// initialized store opening with the conversation it holds.
func TestOpenInitializedConversation_RefusesAnUninitializedStore(t *testing.T) {
	t.Run("absent store refuses and creates no file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "memory.db")
		_, err := sqlite.OpenInitializedConversation(path, wordEmbedder{})
		require.ErrorIs(t, err, sqlite.ErrStoreUninitialized)
		assert.ErrorContains(t, err, path)
		assert.NoFileExists(t, path)

		store, err := sqlite.OpenConversation(path, wordEmbedder{})
		require.NoError(t, err, "control: the same path opens without the check")
		require.NoError(t, store.Close())
		assert.FileExists(t, path)
	})
	t.Run("absent directory refuses and creates no directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "data")
		path := filepath.Join(dir, "memory.db")
		_, err := sqlite.OpenInitializedConversation(path, wordEmbedder{})
		require.ErrorIs(t, err, sqlite.ErrStoreUninitialized)
		assert.ErrorContains(t, err, path)
		assert.NoDirExists(t, dir)
	})
	t.Run("empty file refuses and is left unchanged", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "memory.db")
		require.NoError(t, os.WriteFile(path, nil, 0o600))
		_, err := sqlite.OpenInitializedConversation(path, wordEmbedder{})
		require.ErrorIs(t, err, sqlite.ErrStoreUninitialized)
		assert.ErrorContains(t, err, path)
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Zero(t, info.Size(), "the empty file gained content")
		assert.NoFileExists(t, path+"-wal")

		store, err := sqlite.OpenConversation(path, wordEmbedder{})
		require.NoError(t, err, "control: the same file opens without the check")
		require.NoError(t, store.Close())
		info, err = os.Stat(path)
		require.NoError(t, err)
		assert.NotZero(t, info.Size(), "control: the open without the check initialized the file")
	})
	t.Run("file without a Sherlock schema refuses and is left unchanged", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "memory.db")
		raw, err := sql.Open("sqlite", path)
		require.NoError(t, err)
		_, err = raw.Exec(`CREATE TABLE other (id INTEGER PRIMARY KEY)`)
		require.NoError(t, err)
		require.NoError(t, raw.Close())
		before, err := os.ReadFile(path)
		require.NoError(t, err)

		_, err = sqlite.OpenInitializedConversation(path, wordEmbedder{})
		require.ErrorIs(t, err, sqlite.ErrStoreUninitialized)
		after, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, before, after)

		store, err := sqlite.OpenConversation(path, wordEmbedder{})
		require.NoError(t, err, "control: the same file opens without the check")
		require.NoError(t, store.Close())
	})
	t.Run("initialized store opens with its conversation", func(t *testing.T) {
		ctx := context.Background()
		path := filepath.Join(t.TempDir(), "memory.db")
		store, err := sqlite.OpenConversation(path, wordEmbedder{})
		require.NoError(t, err)
		require.NoError(t, store.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: "remember cedar"}}, firstSummary{}))
		require.NoError(t, store.Close())

		store, err = sqlite.OpenInitializedConversation(path, wordEmbedder{})
		require.NoError(t, err)
		defer store.Close()
		recent, err := store.Recent(ctx, 10)
		require.NoError(t, err)
		require.Len(t, recent, 1)
		assert.Equal(t, "remember cedar", recent[0].Content)
	})
}
