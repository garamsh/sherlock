package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

// scripted answers each synthesis request with the next scripted synthesis
// and keeps the batches it was sent.
type scripted struct {
	syntheses []agent.Synthesis
	sent      [][]string
}

func (s *scripted) Summarize(_ context.Context, updates []string) (agent.Synthesis, error) {
	if len(s.sent) == len(s.syntheses) {
		return agent.Synthesis{}, errors.New("no synthesis left")
	}
	s.sent = append(s.sent, updates)
	return s.syntheses[len(s.sent)-1], nil
}

// linkedStore records one full batch per synthesis, so each becomes one
// mid episode with the synthesis's entities.
func linkedStore(t *testing.T, syntheses ...agent.Synthesis) (*sqlite.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.OpenConversation(path, wordEmbedder{})
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	summarizer := &scripted{syntheses: syntheses}
	for i := range syntheses {
		require.NoError(t, store.RecordTurn(context.Background(), episodeSources(fmt.Sprintf("batch %d", i+1)), summarizer))
	}
	require.Len(t, summarizer.sent, len(syntheses), "the setup: one synthesis per batch")
	return store, path
}

func linkedIDs(linked agent.Linked) []int64 {
	out := []int64{}
	for _, hit := range linked.Hits {
		out = append(out, hit.ID)
	}
	return out
}

func TestLinked_ReadsTheEntitiesTheCheckpointStored(t *testing.T) {
	ctx := context.Background()
	store, path := linkedStore(t,
		agent.Synthesis{Summary: "Every part for the Rodend build comes from Gebind Components.", Entities: []string{"Rodend", "Gebind Components"}},
		agent.Synthesis{Summary: "The user chatted about the weather.", Entities: []string{}},
	)
	linked, err := store.Linked(ctx, "Who supplies the Rodend build?")
	require.NoError(t, err)
	assert.Equal(t, []string{"rodend"}, linked.Seeds)
	require.Equal(t, []int64{1}, linkedIDs(linked))
	assert.Equal(t, "rodend", linked.Hits[0].Subject, "the first name the synthesis wrote")
	assert.Equal(t, "Every part for the Rodend build comes from Gebind Components.", linked.Hits[0].Summary)
	assert.EqualValues(t, 1, linked.Hits[0].SourceFirst)
	assert.EqualValues(t, 4, linked.Hits[0].SourceLast)

	none, err := store.Linked(ctx, "What was the weather like?")
	require.NoError(t, err)
	assert.Empty(t, none.Hits, "the control: a query naming no stored entity reaches nothing")
	assert.Empty(t, none.Seeds)

	require.NoError(t, store.Close())
	reopened, err := sqlite.OpenConversation(path, wordEmbedder{})
	require.NoError(t, err)
	defer reopened.Close()
	again, err := reopened.Linked(ctx, "Who supplies the Rodend build?")
	require.NoError(t, err)
	assert.Equal(t, linked, again, "the links are durable")
	summarizer := &scripted{}
	require.NoError(t, reopened.Link(ctx, summarizer))
	assert.Empty(t, summarizer.sent, "both episodes were linked at checkpoint, the one naming no entity too")
}

func TestLinked_MatchesNamesNormalizedAtCheckpoint(t *testing.T) {
	ctx := context.Background()
	store, _ := linkedStore(t, agent.Synthesis{
		Summary:  "The coach for the Suvun Hotel is booked through Mateo Deatreal.",
		Entities: []string{"The Suvun Hotel's", "Suvun Hotel", "the", "", "Mateo Deatreal’s"},
	})
	for query, seeds := range map[string][]string{
		"Which coach serves the Suvun Hotel?":       {"suvun hotel"},
		"What did SUVUN HOTEL's manager say?":       {"suvun hotel"},
		"Is Mateo Deatreal's number still current?": {"mateo deatreal"},
	} {
		linked, err := store.Linked(ctx, query)
		require.NoError(t, err)
		assert.Equal(t, seeds, linked.Seeds, query)
		require.Equal(t, []int64{1}, linkedIDs(linked), query)
		assert.Equal(t, "suvun hotel", linked.Hits[0].Subject, "%s: a leading stopword and a possessive are dropped, and the repeat is stored once", query)
	}
	for _, query := range []string{"the", "Who looks after the Suvun account?"} {
		linked, err := store.Linked(ctx, query)
		require.NoError(t, err)
		assert.Empty(t, linked.Seeds, "the control: %q holds no whole stored name", query)
	}
}

// preEntityStore writes a file as a store from before entities left it:
// eight sources in two episodes of four, embedded, with no link column and
// no entity table.
func preEntityStore(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memory.db")
	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer raw.Close()
	_, err = raw.Exec(`CREATE TABLE conversation_sources (id INTEGER PRIMARY KEY AUTOINCREMENT, role TEXT NOT NULL CHECK(role IN ('user','assistant')), external_id TEXT, content TEXT NOT NULL);
CREATE TABLE conversation_mid (id INTEGER PRIMARY KEY AUTOINCREMENT, source_first INTEGER NOT NULL UNIQUE, source_last INTEGER NOT NULL UNIQUE, summary TEXT NOT NULL, embedding BLOB);`)
	require.NoError(t, err)
	for i := 1; i <= 8; i++ {
		_, err = raw.Exec(`INSERT INTO conversation_sources(role,external_id,content) VALUES ('user','',?)`, fmt.Sprintf("old-%d", i))
		require.NoError(t, err)
	}
	_, err = raw.Exec(`INSERT INTO conversation_mid(source_first,source_last,summary) VALUES (1,4,'Rodend parts come from Gebind Components.'),(5,8,'The user chatted about the weather.')`)
	require.NoError(t, err)
	return path
}

func TestLink_NamesEachEpisodeFromBeforeEntitiesOnce(t *testing.T) {
	ctx := context.Background()
	path := preEntityStore(t)
	store, err := sqlite.OpenConversation(path, wordEmbedder{})
	require.NoError(t, err)
	defer store.Close()
	before, err := store.Linked(ctx, "Who supplies Rodend?")
	require.NoError(t, err)
	assert.Empty(t, before.Hits, "opening alone links nothing")

	summarizer := &scripted{syntheses: []agent.Synthesis{
		{Summary: "A second summary the store does not keep.", Entities: []string{"Rodend", "Gebind Components"}},
		{Summary: "Weather.", Entities: []string{}},
	}}
	require.NoError(t, store.Link(ctx, summarizer))
	assert.Equal(t, [][]string{{"user: old-1", "user: old-2", "user: old-3", "user: old-4"}}, summarizer.sent, "one pass sends the oldest unlinked episode's own sources")
	linked, err := store.Linked(ctx, "Who supplies Rodend?")
	require.NoError(t, err)
	require.Equal(t, []int64{1}, linkedIDs(linked))
	assert.Equal(t, "Rodend parts come from Gebind Components.", linked.Hits[0].Summary, "the stored summary is kept")

	require.NoError(t, store.Link(ctx, summarizer))
	require.Len(t, summarizer.sent, 2, "the next pass links the next episode, whose synthesis names none")
	require.NoError(t, store.Link(ctx, summarizer))
	assert.Len(t, summarizer.sent, 2, "a pass with nothing left to link makes no call")

	require.NoError(t, store.Close())
	reopened, err := sqlite.OpenConversation(path, wordEmbedder{})
	require.NoError(t, err)
	defer reopened.Close()
	require.NoError(t, reopened.Link(ctx, summarizer))
	assert.Len(t, summarizer.sent, 2, "nor after a restart")
	again, err := reopened.Linked(ctx, "Who supplies Rodend?")
	require.NoError(t, err)
	assert.Equal(t, linked, again)
}

func TestLink_FailedSynthesisLeavesTheEpisodeToTheNextPass(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenConversation(preEntityStore(t), wordEmbedder{})
	require.NoError(t, err)
	defer store.Close()
	require.Error(t, store.Link(ctx, &scripted{}))
	summarizer := &scripted{syntheses: []agent.Synthesis{{Summary: "s", Entities: []string{"Rodend"}}}}
	require.NoError(t, store.Link(ctx, summarizer))
	assert.Equal(t, "user: old-1", summarizer.sent[0][0], "the same episode is sent again")
}
