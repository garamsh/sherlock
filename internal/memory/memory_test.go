package memory_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
	"github.com/garamsh/gagent/internal/tool"
)

var now = time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)

func newService(t *testing.T, opts ...memory.Option) memory.Service {
	t.Helper()
	if len(opts) == 0 {
		opts = []memory.Option{memory.WithClock(func() time.Time { return now })}
	}
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "memory.db"))
	require.NoError(t, err)
	svc := memory.NewService(st, memory.NewHashingEmbedder(), memory.NewConcatSummarizer(), opts...)
	t.Cleanup(func() { assert.NoError(t, svc.Close()) })
	return svc
}

func invoke(t *testing.T, svc memory.Service, input string) json.RawMessage {
	t.Helper()
	out, err := tool.Invoke(context.Background(), memory.NewTool(svc), json.RawMessage(input))
	require.NoError(t, err)
	return out
}

func storeMemory(t *testing.T, svc memory.Service, content, typ string, importance float64) int64 {
	t.Helper()
	res, err := svc.Store(context.Background(), memory.StoreInput{
		Content:         content,
		Type:            memory.Type(typ),
		ImportanceScore: importance,
		Source:          "test",
	})
	require.NoError(t, err)
	require.Positive(t, res.ID)
	return res.ID
}

func searchViaTool(t *testing.T, svc memory.Service, query string) []map[string]any {
	t.Helper()
	body, err := json.Marshal(map[string]any{"op": "search", "query": query})
	require.NoError(t, err)
	out := invoke(t, svc, string(body))
	var res struct {
		Results []map[string]any `json:"results"`
	}
	require.NoError(t, json.Unmarshal(out, &res))
	return res.Results
}

func TestTool_SearchReturnsStoredMemories(t *testing.T) {
	svc := newService(t)
	catID := storeMemory(t, svc, "the cat sat on the mat", "episodic", 0.5)
	storeMemory(t, svc, "quantum entanglement lecture notes", "semantic", 0.5)

	results := searchViaTool(t, svc, "cat")
	require.NotEmpty(t, results)
	assert.Equal(t, float64(catID), results[0]["id"])
	assert.Equal(t, "the cat sat on the mat", results[0]["content"])
	assert.Equal(t, "episodic", results[0]["type"])
	assert.Contains(t, results[0], "score")
	assert.Contains(t, results[0], "timestamp")
}

func TestTool_SearchEmptyStoreReturnsEmptyResults(t *testing.T) {
	svc := newService(t)
	out := invoke(t, svc, `{"op":"search","query":"anything"}`)
	assert.JSONEq(t, `{"results":[]}`, string(out))
}

func TestSearch_RecordsAccess(t *testing.T) {
	svc := newService(t)
	storeMemory(t, svc, "the cat sat on the mat", "episodic", 0.5)

	first, err := svc.Search(context.Background(), "cat", 0)
	require.NoError(t, err)
	require.Len(t, first, 1)
	assert.Zero(t, first[0].AccessCount)

	second, err := svc.Search(context.Background(), "cat", 0)
	require.NoError(t, err)
	require.Len(t, second, 1)
	assert.Equal(t, int64(1), second[0].AccessCount)
	assert.Equal(t, now, second[0].LastAccessed)
}

func TestSimilar_DoesNotRecordAccess(t *testing.T) {
	svc := newService(t)
	storeMemory(t, svc, "the cat sat on the mat", "episodic", 0.5)

	for range 2 {
		results, err := svc.Similar(context.Background(), "cat mat", 0)
		require.NoError(t, err)
		require.Len(t, results, 1)
	}

	results, err := svc.Search(context.Background(), "cat", 0)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Zero(t, results[0].AccessCount)
}

func TestUpdate_SupersedesAndExcludesOldFromSearch(t *testing.T) {
	svc := newService(t)
	oldID := storeMemory(t, svc, "release is on monday", "episodic", 0.7)

	res, err := svc.Update(context.Background(), memory.UpdateInput{
		ID:      oldID,
		Content: "release is on tuesday",
	})
	require.NoError(t, err)
	assert.Equal(t, oldID, res.SupersededID)
	assert.NotEqual(t, oldID, res.ID)

	results := searchViaTool(t, svc, "release monday")
	require.NotEmpty(t, results)
	for _, r := range results {
		assert.NotEqual(t, float64(oldID), r["id"], "archived row must not be searchable")
		assert.NotContains(t, r["content"], "monday")
	}

	_, err = svc.Update(context.Background(), memory.UpdateInput{ID: oldID, Content: "again"})
	require.ErrorIs(t, err, memory.ErrNotFound)
}

func TestDelete_ArchivesAndExcludesFromSearch(t *testing.T) {
	svc := newService(t)
	id := storeMemory(t, svc, "temporary zebra note", "episodic", 0.5)
	keep := storeMemory(t, svc, "durable zebra fact", "semantic", 0.5)

	require.NoError(t, svc.Delete(context.Background(), id))

	results := searchViaTool(t, svc, "zebra")
	require.NotEmpty(t, results, "the surviving memory proves search still matches")
	for _, r := range results {
		assert.NotEqual(t, float64(id), r["id"], "deleted row must not be searchable")
		assert.Equal(t, float64(keep), r["id"])
	}
}

func TestDelete_RecordsValidToWithoutSupersession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	st, err := sqlite.Open(path)
	require.NoError(t, err)
	svc := memory.NewService(st, memory.NewHashingEmbedder(), memory.NewConcatSummarizer(),
		memory.WithClock(func() time.Time { return now }))
	t.Cleanup(func() { assert.NoError(t, svc.Close()) })

	id := storeMemory(t, svc, "temporary note", "episodic", 0.5)
	require.NoError(t, svc.Delete(context.Background(), id))

	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer db.Close()
	var supersededBy sql.NullInt64
	var validTo int64
	require.NoError(t, db.QueryRow(
		`SELECT superseded_by, valid_to FROM memories_archive WHERE id = ?`, id,
	).Scan(&supersededBy, &validTo))
	assert.False(t, supersededBy.Valid, "delete retires; it does not supersede")
	assert.Equal(t, now.Unix(), validTo)
}

func TestDelete_MissingOrAlreadyArchivedReturnsErrNotFound(t *testing.T) {
	svc := newService(t)
	id := storeMemory(t, svc, "note", "episodic", 0.5)

	require.NoError(t, svc.Delete(context.Background(), id))
	err := svc.Delete(context.Background(), id)
	require.ErrorIs(t, err, memory.ErrNotFound, "delete is not idempotent")

	err = svc.Delete(context.Background(), 999)
	require.ErrorIs(t, err, memory.ErrNotFound)
}

func TestUpdate_InheritsImportanceWhenOmitted(t *testing.T) {
	svc := newService(t)
	oldID := storeMemory(t, svc, "release is on monday", "episodic", 0.7)

	res, err := svc.Update(context.Background(), memory.UpdateInput{ID: oldID, Content: "release is on tuesday"})
	require.NoError(t, err)

	results, err := svc.Similar(context.Background(), "release tuesday", 0)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, res.ID, results[0].ID)
	assert.InDelta(t, 0.7, results[0].ImportanceScore, 1e-9)
}

func TestCompress_SummarizesEpisodicIntoSemanticParent(t *testing.T) {
	svc := newService(t)
	id1 := storeMemory(t, svc, "deployed v1 to staging", "episodic", 0.4)
	id2 := storeMemory(t, svc, "staging deploy failed on migration", "episodic", 0.8)
	keep := storeMemory(t, svc, "unrelated fact about cats", "episodic", 0.3)

	res, err := svc.Compress(context.Background(), []int64{id1, id2})
	require.NoError(t, err)
	assert.ElementsMatch(t, []int64{id1, id2}, res.ArchivedIDs)

	results := searchViaTool(t, svc, "staging deploy")
	require.NotEmpty(t, results)
	ids := make([]float64, 0, len(results))
	for _, r := range results {
		ids = append(ids, r["id"].(float64))
	}
	assert.Contains(t, ids, float64(res.ParentID))
	assert.Contains(t, ids, float64(keep))
	assert.NotContains(t, ids, float64(id1))
	assert.NotContains(t, ids, float64(id2))

	for _, r := range results {
		if r["id"].(float64) == float64(res.ParentID) {
			assert.Equal(t, "semantic", r["type"])
			assert.Contains(t, r["content"], "deployed v1 to staging")
			assert.InDelta(t, 0.8, r["importance_score"], 1e-9)
		}
	}
}

func TestCompress_RejectsSemanticInput(t *testing.T) {
	svc := newService(t)
	id := storeMemory(t, svc, "durable knowledge", "semantic", 0.9)

	_, err := svc.Compress(context.Background(), []int64{id})
	require.ErrorIs(t, err, memory.ErrNotEpisodic)
}

func TestForget_ArchivesBelowThreshold(t *testing.T) {
	clock := now
	svc := newService(t, memory.WithClock(func() time.Time { return clock }))
	oldID := storeMemory(t, svc, "stale trivia", "episodic", 0)

	clock = now.Add(30 * 24 * time.Hour)
	freshID := storeMemory(t, svc, "fresh important fact", "semantic", 0.9)

	archived, err := svc.Forget(context.Background(), 0.5)
	require.NoError(t, err)
	assert.Contains(t, archived, oldID)
	assert.NotContains(t, archived, freshID)

	results := searchViaTool(t, svc, "stale trivia")
	for _, r := range results {
		assert.NotEqual(t, float64(oldID), r["id"])
	}
}

func TestStore_EvictsLowestScoreBeyondCapacity(t *testing.T) {
	svc := newService(t,
		memory.WithClock(func() time.Time { return now }),
		memory.WithCapacity(3))

	var ids []int64
	for i, importance := range []float64{0.1, 0.9, 0.5, 0.7} {
		res, err := svc.Store(context.Background(), memory.StoreInput{
			Content:         "memory number " + string(rune('a'+i)),
			Type:            memory.TypeEpisodic,
			ImportanceScore: importance,
			Source:          "test",
		})
		require.NoError(t, err)
		ids = append(ids, res.ID)
		if i == 3 {
			assert.Equal(t, []int64{ids[0]}, res.EvictedIDs, "lowest importance is evicted first")
		}
	}

	active, err := svc.Search(context.Background(), "memory number", 10)
	require.NoError(t, err)
	require.Len(t, active, 3)
	for _, m := range active {
		assert.NotEqual(t, ids[0], m.ID)
	}
}

func TestRecall_TrimsToTokenBudget(t *testing.T) {
	svc := newService(t)
	for range 5 {
		storeMemory(t, svc, "a fairly long memory about cats and mats and sitting", "episodic", 0.5)
	}

	results, err := svc.Recall(context.Background(), "cats", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1, "budget fits fewer than all; top entry is always kept")

	all, err := svc.Recall(context.Background(), "cats", 100_000)
	require.NoError(t, err)
	assert.Len(t, all, 5)
}

func TestTool_RejectsInvalidInput(t *testing.T) {
	svc := newService(t)
	tl := memory.NewTool(svc)

	tests := []struct {
		name  string
		input string
	}{
		{"unknown op", `{"op":"explode"}`},
		{"store op removed", `{"op":"store","content":"x","type":"episodic","importance_score":0.5,"source":"test"}`},
		{"update op removed", `{"op":"update","id":1,"content":"x"}`},
		{"compress op removed", `{"op":"compress","ids":[1]}`},
		{"forget op removed", `{"op":"forget","threshold":0.5}`},
		{"search missing query", `{"op":"search"}`},
		{"search limit below minimum", `{"op":"search","query":"x","limit":0}`},
		{"unexpected field", `{"op":"search","query":"x","content":"y"}`},
		{"undeclared field", `{"op":"search","query":"x","nonsense":"y"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Invoke(context.Background(), tl, json.RawMessage(tc.input))
			require.ErrorIs(t, err, tool.ErrValidation)
		})
	}
}

// Models generate tool arguments from the advertised root properties, so every
// field and every operation name must be visible there, not only in conditionals.
func TestTool_InputSchemaAdvertisesEveryFieldAtRoot(t *testing.T) {
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(memory.NewTool(nil).InputSchema(), &schema))

	for _, field := range []string{"op", "query", "limit"} {
		assert.Contains(t, schema.Properties, field)
	}
	assert.Equal(t, []string{"search"}, schema.Properties["op"].Enum,
		"ADR 0015: the memory tool exposes only the search op")
}

// TestTool_DescriptionStatesReadOnly pins the #160 model-facing fix: the
// advertised description tells the model the tool is read-only and that
// recording happens in the writeback block, so a "remember X" request
// does not loop searches (ADR 0015).
func TestTool_DescriptionStatesReadOnly(t *testing.T) {
	desc := memory.NewTool(nil).Description()

	assert.Contains(t, desc, "Search", "the description names the search capability")
	assert.Contains(t, desc, "Read-only", "the description states the tool cannot store")
	assert.Contains(t, desc, "writeback block",
		"the description points recording at the writeback block")
}

func TestService_RejectsInvalidInput(t *testing.T) {
	svc := newService(t)

	_, err := svc.Store(context.Background(), memory.StoreInput{
		Content: "", Type: memory.TypeEpisodic, Source: "test",
	})
	require.ErrorIs(t, err, memory.ErrInvalidInput)

	_, err = svc.Store(context.Background(), memory.StoreInput{
		Content: "x", Type: "bogus", Source: "test",
	})
	require.ErrorIs(t, err, memory.ErrInvalidInput)

	_, err = svc.Compress(context.Background(), nil)
	require.ErrorIs(t, err, memory.ErrInvalidInput)
}

func TestTool_ExecutionErrorWrapsServiceFailure(t *testing.T) {
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "memory.db"))
	require.NoError(t, err)
	svc := memory.NewService(st, memory.NewHashingEmbedder(), memory.NewConcatSummarizer())
	require.NoError(t, svc.Close(), "a closed store fails every search")

	_, err = tool.Invoke(context.Background(), memory.NewTool(svc),
		json.RawMessage(`{"op":"search","query":"x"}`))
	require.Error(t, err)
	assert.True(t, errors.Is(err, tool.ErrExecution))
}

// TestConsolidate_CompressesEpisodicCluster pins the ADR 0015 platform
// trigger: once at least ConsolidationMinCluster episodic memories are
// active, Consolidate compresses them into one semantic parent and
// archives the originals.
func TestConsolidate_CompressesEpisodicCluster(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	st, err := sqlite.Open(path)
	require.NoError(t, err)
	svc := memory.NewService(st, memory.NewHashingEmbedder(), memory.NewConcatSummarizer(),
		memory.WithClock(func() time.Time { return now }))
	t.Cleanup(func() { assert.NoError(t, svc.Close()) })

	id1 := storeMemory(t, svc, "deployed v1 to staging", "episodic", 0.4)
	id2 := storeMemory(t, svc, "staging deploy failed on migration", "episodic", 0.8)
	keep := storeMemory(t, svc, "durable knowledge about cats", "semantic", 0.9)

	res, err := svc.Consolidate(context.Background())
	require.NoError(t, err)
	require.Positive(t, res.ParentID)
	assert.ElementsMatch(t, []int64{id1, id2}, res.ArchivedIDs)

	results, err := svc.Search(context.Background(), "staging deploy", 10)
	require.NoError(t, err)
	ids := make([]int64, 0, len(results))
	for _, r := range results {
		ids = append(ids, r.ID)
	}
	assert.Contains(t, ids, res.ParentID, "the semantic parent is searchable")
	assert.NotContains(t, ids, id1, "archived originals leave the active index")
	assert.NotContains(t, ids, id2, "archived originals leave the active index")

	cats, err := svc.Search(context.Background(), "durable knowledge cats", 10)
	require.NoError(t, err)
	require.NotEmpty(t, cats)
	assert.Equal(t, keep, cats[0].ID, "semantic memories are not consolidated")

	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer db.Close()
	for _, id := range []int64{id1, id2} {
		var supersededBy int64
		require.NoError(t, db.QueryRow(
			`SELECT superseded_by FROM memories_archive WHERE id = ?`, id,
		).Scan(&supersededBy))
		assert.Equal(t, res.ParentID, supersededBy,
			"the archived original points at its semantic parent")
	}
}

func TestConsolidate_BelowClusterIsNoOp(t *testing.T) {
	svc := newService(t)
	id := storeMemory(t, svc, "lone episodic note", "episodic", 0.5)

	res, err := svc.Consolidate(context.Background())
	require.NoError(t, err)
	assert.Zero(t, res.ParentID)
	assert.Empty(t, res.ArchivedIDs)

	results, err := svc.Search(context.Background(), "lone episodic", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, id, results[0].ID, "the single episodic memory stays active")
}

func TestHashingEmbedder_IsDeterministicAndNormalized(t *testing.T) {
	emb := memory.NewHashingEmbedder()
	a, err := emb.Embed(context.Background(), "the cat sat")
	require.NoError(t, err)
	b, err := emb.Embed(context.Background(), "the cat sat")
	require.NoError(t, err)
	assert.Equal(t, a, b)
	assert.Len(t, a, memory.EmbeddingDim)

	var norm float64
	for _, v := range a {
		norm += float64(v) * float64(v)
	}
	assert.InDelta(t, 1.0, norm, 1e-6)
}
