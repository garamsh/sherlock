package memory_test

import (
	"context"
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

func storeViaTool(t *testing.T, svc memory.Service, content, typ string, importance float64) int64 {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"op": "store", "content": content, "type": typ,
		"importance_score": importance, "source": "test",
	})
	require.NoError(t, err)
	out := invoke(t, svc, string(body))
	var res struct {
		ID int64 `json:"id"`
	}
	require.NoError(t, json.Unmarshal(out, &res))
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

func TestTool_StoreAndSearch(t *testing.T) {
	svc := newService(t)
	catID := storeViaTool(t, svc, "the cat sat on the mat", "episodic", 0.5)
	storeViaTool(t, svc, "quantum entanglement lecture notes", "semantic", 0.5)

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
	storeViaTool(t, svc, "the cat sat on the mat", "episodic", 0.5)

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
	storeViaTool(t, svc, "the cat sat on the mat", "episodic", 0.5)

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

func TestTool_UpdateSupersedesAndExcludesOldFromSearch(t *testing.T) {
	svc := newService(t)
	oldID := storeViaTool(t, svc, "release is on monday", "episodic", 0.7)

	body, err := json.Marshal(map[string]any{
		"op": "update", "id": oldID, "content": "release is on tuesday",
	})
	require.NoError(t, err)
	out := invoke(t, svc, string(body))
	var res struct {
		ID           int64 `json:"id"`
		SupersededID int64 `json:"superseded_id"`
	}
	require.NoError(t, json.Unmarshal(out, &res))
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

func TestUpdate_InheritsImportanceWhenOmitted(t *testing.T) {
	svc := newService(t)
	oldID := storeViaTool(t, svc, "release is on monday", "episodic", 0.7)

	res, err := svc.Update(context.Background(), memory.UpdateInput{ID: oldID, Content: "release is on tuesday"})
	require.NoError(t, err)

	results, err := svc.Similar(context.Background(), "release tuesday", 0)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, res.ID, results[0].ID)
	assert.InDelta(t, 0.7, results[0].ImportanceScore, 1e-9)
}

func TestTool_Compress(t *testing.T) {
	svc := newService(t)
	id1 := storeViaTool(t, svc, "deployed v1 to staging", "episodic", 0.4)
	id2 := storeViaTool(t, svc, "staging deploy failed on migration", "episodic", 0.8)
	keep := storeViaTool(t, svc, "unrelated fact about cats", "episodic", 0.3)

	body, err := json.Marshal(map[string]any{"op": "compress", "ids": []int64{id1, id2}})
	require.NoError(t, err)
	out := invoke(t, svc, string(body))
	var res struct {
		ParentID    int64   `json:"parent_id"`
		ArchivedIDs []int64 `json:"archived_ids"`
	}
	require.NoError(t, json.Unmarshal(out, &res))
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
	id := storeViaTool(t, svc, "durable knowledge", "semantic", 0.9)

	_, err := svc.Compress(context.Background(), []int64{id})
	require.ErrorIs(t, err, memory.ErrNotEpisodic)
}

func TestTool_Forget(t *testing.T) {
	clock := now
	svc := newService(t, memory.WithClock(func() time.Time { return clock }))
	oldID := storeViaTool(t, svc, "stale trivia", "episodic", 0)

	clock = now.Add(30 * 24 * time.Hour)
	freshID := storeViaTool(t, svc, "fresh important fact", "semantic", 0.9)

	out := invoke(t, svc, `{"op":"forget","threshold":0.5}`)
	var res struct {
		ArchivedIDs []int64 `json:"archived_ids"`
	}
	require.NoError(t, json.Unmarshal(out, &res))
	assert.Contains(t, res.ArchivedIDs, oldID)
	assert.NotContains(t, res.ArchivedIDs, freshID)

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
		storeViaTool(t, svc, "a fairly long memory about cats and mats and sitting", "episodic", 0.5)
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
		{"store missing content", `{"op":"store","type":"episodic","importance_score":0.5,"source":"test"}`},
		{"store bad type", `{"op":"store","content":"x","type":"bogus","importance_score":0.5,"source":"test"}`},
		{"store importance out of range", `{"op":"store","content":"x","type":"episodic","importance_score":1.5,"source":"test"}`},
		{"search missing query", `{"op":"search"}`},
		{"update missing id", `{"op":"update","content":"x"}`},
		{"compress empty ids", `{"op":"compress","ids":[]}`},
		{"unexpected field", `{"op":"forget","query":"x"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Invoke(context.Background(), tl, json.RawMessage(tc.input))
			require.ErrorIs(t, err, tool.ErrValidation)
		})
	}
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
	svc := newService(t)
	_, err := tool.Invoke(context.Background(), memory.NewTool(svc),
		json.RawMessage(`{"op":"update","id":999,"content":"x"}`))
	require.Error(t, err)
	assert.True(t, errors.Is(err, tool.ErrExecution))
	assert.True(t, errors.Is(err, memory.ErrNotFound))
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
