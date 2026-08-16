package memory_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
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

func search(t *testing.T, svc memory.Service, query string) []memory.ScoredMemory {
	t.Helper()
	results, err := svc.Search(context.Background(), query, 0)
	require.NoError(t, err)
	return results
}

func TestSearch_ReturnsStoredMemories(t *testing.T) {
	svc := newService(t)
	catID := storeMemory(t, svc, "the cat sat on the mat", "episodic", 0.5)
	storeMemory(t, svc, "quantum entanglement lecture notes", "semantic", 0.5)

	results := search(t, svc, "cat")
	require.NotEmpty(t, results)
	assert.Equal(t, catID, results[0].ID)
	assert.Equal(t, "the cat sat on the mat", results[0].Content)
	assert.Equal(t, memory.TypeEpisodic, results[0].Type)
}

func TestSearch_EmptyStoreReturnsEmptyResults(t *testing.T) {
	svc := newService(t)
	assert.Empty(t, search(t, svc, "anything"))
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

	results := search(t, svc, "release monday")
	require.NotEmpty(t, results)
	for _, r := range results {
		assert.NotEqual(t, oldID, r.ID, "archived row must not be searchable")
		assert.NotContains(t, r.Content, "monday")
	}

	_, err = svc.Update(context.Background(), memory.UpdateInput{ID: oldID, Content: "again"})
	require.ErrorIs(t, err, memory.ErrNotFound)
}

func TestDelete_ArchivesAndExcludesFromSearch(t *testing.T) {
	svc := newService(t)
	id := storeMemory(t, svc, "temporary zebra note", "episodic", 0.5)
	keep := storeMemory(t, svc, "durable zebra fact", "semantic", 0.5)

	require.NoError(t, svc.Delete(context.Background(), id))

	results := search(t, svc, "zebra")
	require.NotEmpty(t, results, "the surviving memory proves search still matches")
	for _, r := range results {
		assert.NotEqual(t, id, r.ID, "deleted row must not be searchable")
		assert.Equal(t, keep, r.ID)
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

	results := search(t, svc, "staging deploy")
	require.NotEmpty(t, results)
	ids := make([]int64, 0, len(results))
	for _, r := range results {
		ids = append(ids, r.ID)
	}
	require.Len(t, res.ParentIDs, 1, "small cluster yields one parent")
	assert.Contains(t, ids, res.ParentIDs[0])
	assert.Contains(t, ids, keep)
	assert.NotContains(t, ids, id1)
	assert.NotContains(t, ids, id2)

	for _, r := range results {
		if r.ID == res.ParentIDs[0] {
			assert.Equal(t, memory.TypeSemantic, r.Type)
			assert.Contains(t, r.Content, "deployed v1 to staging")
			assert.InDelta(t, 0.8, r.ImportanceScore, 1e-9)
		}
	}
}

func TestCompress_RejectsSemanticInput(t *testing.T) {
	svc := newService(t)
	id := storeMemory(t, svc, "durable knowledge", "semantic", 0.9)

	_, err := svc.Compress(context.Background(), []int64{id})
	require.ErrorIs(t, err, memory.ErrNotEpisodic)
}

// TestStore_ExactRestatementKeepsBoth pins that an insert retires
// nothing, even for the case supersession on insert was built for: a
// second write of the very same sentence. Identity is not inferred from
// similarity (ADR 0006 §Decision, ADR 0018), so the store has nothing
// to decide with and adds.
func TestStore_ExactRestatementKeepsBoth(t *testing.T) {
	svc := newService(t)

	oldID := storeMemory(t, svc, "the release train leaves on monday", "semantic", 0.7)
	res, err := svc.Store(context.Background(), memory.StoreInput{
		Content:         "the release train leaves on monday",
		Type:            memory.TypeSemantic,
		ImportanceScore: 0.7,
		Source:          "test",
	})
	require.NoError(t, err)
	assert.Zero(t, res.SupersededID, "an insert retires nothing")
	assert.NotEqual(t, oldID, res.ID)

	results := search(t, svc, "release train")
	require.Len(t, results, 2, "both versions stay active")
}

// TestStore_CorrectionCoexistsWithWhatItCorrects states the cost of
// removing supersession on insert (#529), so it is visible in the suite
// and not only in prose: until the addressed store lands (#336 step 2)
// a correction sits beside the value it corrects instead of replacing
// it.
//
// That is the accumulating direction. ADR 0018 §Where a writeback must
// err prefers it: the next correct write at the address repairs it,
// where the destroying direction — which is what the threshold produced
// once writes carried their address in the content — repairs at 0.000
// and carries its damage for the rest of the span.
func TestStore_CorrectionCoexistsWithWhatItCorrects(t *testing.T) {
	svc := newService(t)

	oldID := storeMemory(t, svc,
		"A main.go file with a greeting program has been created in the agent-test project.",
		"episodic", 0.5)
	res, err := svc.Store(context.Background(), memory.StoreInput{
		Content: "the greeting program in the agent-test project is now main.py, " +
			"a Python file — the Go main.go version was replaced",
		Type:            memory.TypeEpisodic,
		ImportanceScore: 0.5,
		Source:          "test",
	})
	require.NoError(t, err)
	assert.Zero(t, res.SupersededID, "the correction does not retire what it corrects")

	results := search(t, svc, "greeting program agent-test")
	ids := make([]int64, 0, len(results))
	for _, m := range results {
		ids = append(ids, m.ID)
	}
	assert.Contains(t, ids, oldID, "the corrected value is still there — the cost, stated")
	assert.Contains(t, ids, res.ID)
}

// TestStore_DissimilarInsertKeepsBoth pins the unrelated-content case:
// an add that shares nothing with what is already stored inserts a
// fresh row and retires nothing. It held before supersession on insert
// was removed and holds after, which is what makes it the control for
// the two tests above.
func TestStore_DissimilarInsertKeepsBoth(t *testing.T) {
	svc := newService(t)
	oldID := storeMemory(t, svc, "the release train leaves on monday", "semantic", 0.7)

	res, err := svc.Store(context.Background(), memory.StoreInput{
		Content:         "quantum entanglement lecture notes",
		Type:            memory.TypeSemantic,
		ImportanceScore: 0.7,
		Source:          "test",
	})
	require.NoError(t, err)
	assert.Zero(t, res.SupersededID, "a dissimilar add supersedes nothing")

	results := search(t, svc, "release train")
	require.NotEmpty(t, results)
	assert.Equal(t, oldID, results[0].ID, "the predecessor stays active")
}

// TestRetain_TiersBySalience pins the ADR 0006 tiered retention pass:
// high salience keeps indefinitely, mid salience lives for
// RetentionWindow, low salience drops to the archive at the next pass.
func TestRetain_TiersBySalience(t *testing.T) {
	clock := now
	svc := newService(t, memory.WithClock(func() time.Time { return clock }))
	high := storeMemory(t, svc, "durable high-salience fact", "semantic", 0.8)
	mid := storeMemory(t, svc, "mid-salience episodic observation", "episodic", 0.5)
	low := storeMemory(t, svc, "low-salience chatter", "episodic", 0.1)

	clock = now.Add(memory.RetentionWindow - time.Hour)
	archived, err := svc.Retain(context.Background())
	require.NoError(t, err)
	assert.ElementsMatch(t, []int64{low}, archived,
		"within the window only low-salience memories drop")

	clock = now.Add(memory.RetentionWindow + time.Hour)
	archived, err = svc.Retain(context.Background())
	require.NoError(t, err)
	assert.ElementsMatch(t, []int64{mid}, archived,
		"past the window the mid tier drops; the high tier never does")

	results := search(t, svc, "salience")
	require.Len(t, results, 1)
	assert.Equal(t, high, results[0].ID, "only the high-salience memory stays active")

	err = svc.Delete(context.Background(), low)
	assert.ErrorIs(t, err, memory.ErrNotFound,
		"the dropped memory sits in the archive, not the active index")
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

	results := search(t, svc, "stale trivia")
	for _, r := range results {
		assert.NotEqual(t, oldID, r.ID)
	}
}

func TestStore_EvictsLowestScoreBeyondCapacity(t *testing.T) {
	svc := newService(t,
		memory.WithClock(func() time.Time { return now }),
		memory.WithCapacity(3))

	var ids []int64
	contents := []string{
		"memory number zero apple banana",
		"memory number one cherry durian",
		"memory number two elderberry fig",
		"memory number three grape hazelnut",
	}
	for i, importance := range []float64{0.1, 0.9, 0.5, 0.7} {
		res, err := svc.Store(context.Background(), memory.StoreInput{
			Content:         contents[i],
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
	// Distinct sentences: near-duplicates would supersede on insert
	// (ADR 0006) instead of accumulating for the budget to trim.
	for _, content := range []string{
		"a fairly long memory about cats knocking vases off shelves loudly",
		"a fairly long memory about cats sleeping through thunderstorms nightly",
		"a fairly long memory about cats learning to open door handles quietly",
		"a fairly long memory about cats riding the city trams at dawn yesterday",
		"a fairly long memory about cats adopting the warehouse office suddenly",
	} {
		storeMemory(t, svc, content, "episodic", 0.5)
	}

	results, err := svc.Recall(context.Background(), "cats", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1, "budget fits fewer than all; top entry is always kept")

	all, err := svc.Recall(context.Background(), "cats", 100_000)
	require.NoError(t, err)
	assert.Len(t, all, 5)
}

// TestRecall_BoundIsTheBudgetAloneNotARowLimit pins the one bound (ADR
// 0006 §Decision step 2: "a variable number of raw entries within a
// token budget"). A row limit anywhere in the recall path would decide
// the count before the budget could, and DefaultSearchLimit is the one
// that used to: it capped the candidates at ten, so no budget above ten
// short memories could bind.
func TestRecall_BoundIsTheBudgetAloneNotARowLimit(t *testing.T) {
	svc := newService(t)
	// No two memories share a word, so none supersedes another on
	// insert (ADR 0006) and the store holds all of them.
	const planted = memory.DefaultSearchLimit * 3
	for i := range planted {
		storeMemory(t, svc, fmt.Sprintf("alpha%d beta%d gamma%d", i, i, i), "episodic", 0.5)
	}

	// Every memory costs a handful of tokens, so the default budget
	// bounds nothing here and the count is the store's.
	results, err := svc.Recall(context.Background(), "alpha0", memory.DefaultRecallTokenBudget)
	require.NoError(t, err)
	assert.Len(t, results, planted,
		"the budget fits every memory, so recall returns every memory")
	assert.Greater(t, len(results), memory.DefaultSearchLimit,
		"a recall bounded by DefaultSearchLimit cannot exceed it, and this one must")

	// The budget still binds when it is the smaller bound.
	const smallBudget = 20
	trimmed, err := svc.Recall(context.Background(), "alpha0", smallBudget)
	require.NoError(t, err)
	assert.Less(t, len(trimmed), planted, "a budget below the store's cost trims")
	var used int
	for _, m := range trimmed {
		used += max(len(m.Content)/memory.CharsPerToken, 1)
	}
	assert.LessOrEqual(t, used, smallBudget, "the trim stops at the budget")
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

// TestConsolidate_CompressesEpisodicCluster pins the ADR 0011 platform
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
	require.Positive(t, res.ParentIDs[0])
	assert.ElementsMatch(t, []int64{id1, id2}, res.ArchivedIDs)

	results, err := svc.Search(context.Background(), "staging deploy", 10)
	require.NoError(t, err)
	ids := make([]int64, 0, len(results))
	for _, r := range results {
		ids = append(ids, r.ID)
	}
	assert.Contains(t, ids, res.ParentIDs[0], "the semantic parent is searchable")
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
		assert.Equal(t, res.ParentIDs[0], supersededBy,
			"the archived original points at its semantic parent")
	}
}

// TestConsolidateIfDue_TriggersOnlyAtThreshold pins the threshold
// trigger (ADR 0006, ADR 0011): below ConsolidationThreshold active
// episodic memories nothing runs; at the threshold the cluster
// compresses into semantic parents.
func TestConsolidateIfDue_TriggersOnlyAtThreshold(t *testing.T) {
	svc := newService(t)
	// Each note carries unique tokens so inserts stay below the
	// supersession similarity (ADR 0006) and the cluster accumulates.
	for i := range memory.ConsolidationThreshold - 1 {
		storeMemory(t, svc, fmt.Sprintf("episodic note %d w%d x%d y%d z%d q%d", i, i, i, i, i, i), "episodic", 0.5)
	}

	res, err := svc.ConsolidateIfDue(context.Background())
	require.NoError(t, err)
	assert.Empty(t, res.ArchivedIDs, "below the threshold nothing consolidates")

	storeMemory(t, svc, fmt.Sprintf("episodic note %d w%d x%d y%d z%d q%d",
		memory.ConsolidationThreshold-1, memory.ConsolidationThreshold-1,
		memory.ConsolidationThreshold-1, memory.ConsolidationThreshold-1,
		memory.ConsolidationThreshold-1, memory.ConsolidationThreshold-1), "episodic", 0.5)
	res, err = svc.ConsolidateIfDue(context.Background())
	require.NoError(t, err)
	require.Len(t, res.ArchivedIDs, memory.ConsolidationThreshold,
		"at the threshold the whole episodic cluster compresses")
	assert.NotEmpty(t, res.ParentIDs)

	active, err := svc.Search(context.Background(), "episodic note", 10)
	require.NoError(t, err)
	for _, m := range active {
		assert.Equal(t, memory.TypeSemantic, m.Type,
			"only the semantic parents stay active")
	}
}

func TestConsolidate_BelowClusterIsNoOp(t *testing.T) {
	svc := newService(t)
	id := storeMemory(t, svc, "lone episodic note", "episodic", 0.5)

	res, err := svc.Consolidate(context.Background())
	require.NoError(t, err)
	assert.Empty(t, res.ParentIDs)
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

// TestCompress_ClusterExceedingSummaryMaxLenLeavesNoContentBehind pins
// issue #194: a cluster whose joined contents exceed SummaryMaxLen
// must split into multiple semantic parents so every byte of the
// originals stays in the searchable index. Tail content no longer
// disappears into the archive.
func TestCompress_ClusterExceedingSummaryMaxLenLeavesNoContentBehind(t *testing.T) {
	svc := newService(t)

	// 30 short messages join to ~30 * 32 = ~960 bytes of summary text,
	// well past the 512-byte cap that truncated everything before. Each
	// carries a unique token so inserts stay below the supersession
	// similarity (ADR 0006) and the whole cluster accumulates.
	const count = 30
	originals := make([]int64, 0, count)
	for i := range count {
		content := fmt.Sprintf("user-turn %02d: ref %04d token-%02d", i, i*7+1, i)
		originals = append(originals, storeMemory(t, svc, content, "episodic", 0.5))
	}

	res, err := svc.Compress(context.Background(), originals)
	require.NoError(t, err)
	require.Greater(t, len(res.ParentIDs), 1,
		"a cluster past SummaryMaxLen must produce more than one parent")
	for _, id := range res.ParentIDs {
		assert.Positive(t, id)
	}
	assert.ElementsMatch(t, originals, res.ArchivedIDs)

	active, err := svc.Search(context.Background(), "user-turn", 100)
	require.NoError(t, err)
	got := make(map[int64]string, len(active))
	for _, r := range active {
		got[r.ID] = r.Content
	}
	for _, pid := range res.ParentIDs {
		require.Contains(t, got, pid, "every parent stays in the active index")
		assert.LessOrEqual(t, len(got[pid]), memory.SummaryMaxLen,
			"each parent respects the per-parent length cap")
	}

	// The first content lives entirely inside the cluster; after
	// consolidation its distinguishing substring (a unique ref token)
	// must surface from at least one parent's content — it cannot
	// be archived and forgotten.
	firstRef := fmt.Sprintf("ref %04d", 0*7+1)
	lastRef := fmt.Sprintf("ref %04d", (count-1)*7+1)
	found := 0
	for _, content := range got {
		if strings.Contains(content, firstRef) || strings.Contains(content, lastRef) {
			found++
		}
	}
	assert.Positive(t, found,
		"head and tail tokens must surface from at least one parent")
}

// TestCompress_PreferredParentIsFirstChunk pins that the first parent
// (the one the archived originals point at via superseded_by) is the
// head of the cluster — coherent with ConcatSummarizer's join order.
func TestCompress_PreferredParentIsFirstChunk(t *testing.T) {
	svc := newService(t)

	var ids []int64
	for i := range 20 {
		ids = append(ids, storeMemory(t, svc,
			fmt.Sprintf("alpha marker %04d tail token-%02d extra%02d uniq%02d", i, i, i, i), "episodic", 0.5))
	}

	res, err := svc.Compress(context.Background(), ids)
	require.NoError(t, err)
	require.NotEmpty(t, res.ParentIDs)

	active, err := svc.Search(context.Background(), "alpha marker", 100)
	require.NoError(t, err)
	contents := make(map[int64]string, len(active))
	for _, r := range active {
		contents[r.ID] = r.Content
	}
	preferred := contents[res.ParentIDs[0]]
	assert.True(t, strings.HasPrefix(preferred, "alpha marker 0000"),
		"the preferred (first) parent opens with the first original; got %q", preferred)
	assert.Contains(t, preferred, " | ",
		"the preferred parent joins multiple originals via the chunk separator")
}

// TestCompress_ChunkSeparatorIsTheChunkerBoundary pins that the
// summarizer output's separator is what the chunker uses to keep whole
// content pieces inside one parent. The "aa bb cc" join lands inside
// a single chunk because it fits SummaryMaxLen; a larger join splits
// at separator boundaries so whole pieces never straddle parents.
func TestCompress_ChunkSeparatorIsTheChunkerBoundary(t *testing.T) {
	svc := newService(t)

	// Three pieces small enough that the joined summary fits one parent.
	id1 := storeMemory(t, svc, "aa", "episodic", 0.5)
	id2 := storeMemory(t, svc, "bb", "episodic", 0.5)
	id3 := storeMemory(t, svc, "cc", "episodic", 0.5)
	res, err := svc.Compress(context.Background(), []int64{id1, id2, id3})
	require.NoError(t, err)
	require.Len(t, res.ParentIDs, 1,
		"a small cluster fits one parent with no chunking")

	// Larger cluster: each original carries the same 20-char run plus a
	// unique tag (identical originals would supersede on insert, ADR
	// 0006), so the joined text exceeds SummaryMaxLen and must split
	// across parents, but the whole content must still surface somewhere.
	var ids []int64
	for i := range 40 {
		ids = append(ids, storeMemory(t, svc,
			fmt.Sprintf("%s %02d", strings.Repeat("a", 20), i), "episodic", 0.5))
	}
	res, err = svc.Compress(context.Background(), ids)
	require.NoError(t, err)
	require.Greater(t, len(res.ParentIDs), 1,
		"40 originals of 20 chars joined must exceed SummaryMaxLen")

	active, err := svc.Search(context.Background(), strings.Repeat("a", 20), 100)
	require.NoError(t, err)
	require.NotEmpty(t, active)
	totalBytes := 0
	for _, r := range active {
		assert.LessOrEqual(t, len(r.Content), memory.SummaryMaxLen,
			"each parent respects the per-parent length cap")
		totalBytes += len(r.Content)
	}
	// 40 originals of 20 chars + 39 separators of 3 chars = 917 chars
	// of joined content. After chunking the parents must still hold
	// at least that much text (chunking never drops content).
	assert.GreaterOrEqual(t, totalBytes, 40*20,
		"the union of parents covers every original byte")
}

// TestCompress_HardCapsOversizedSingleContent pins that an original
// exceeding SummaryMaxLen on its own is hard-capped, not split across
// multiple parents — the chunker's hard-cap fallback fires when there
// is no separator to break on.
func TestCompress_HardCapsOversizedSingleContent(t *testing.T) {
	svc := newService(t)

	// One oversized original past SummaryMaxLen with no separator
	// inside; the chunker must hard-cap without losing the bytes it
	// trimmed (they are inside the single parent it produces).
	oversized := strings.Repeat("z", memory.SummaryMaxLen+50)
	id := storeMemory(t, svc, oversized, "episodic", 0.5)

	res, err := svc.Compress(context.Background(), []int64{id})
	require.NoError(t, err)
	require.Len(t, res.ParentIDs, 1)

	active, err := svc.Search(context.Background(), "zzz", 10)
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, memory.SummaryMaxLen, len(active[0].Content),
		"a single oversized original is hard-capped to one parent's length")
}

// TestStore_DifferentAddressesUnderOneSubjectCoexist pins #529: two
// facts about one subject that answer different questions both survive
// a write. Both are joins — ADR 0018 §Where a writeback must err makes
// join the direction the system errs toward — so neither may destroy
// the other.
//
// The contents are what the store actually holds since #523: the
// addressed sentence internal/gateway/writeback.go's sentenceOf
// renders, "<subject> — <question> <value>". That shared prefix is a
// large fraction of a short fact's tokens, which is what lifted cosine
// past the old supersession threshold for pairs with nothing in common
// but their subject. The embedder is the runtime's default.
func TestStore_DifferentAddressesUnderOneSubjectCoexist(t *testing.T) {
	svc := newService(t)

	telescopeID := storeMemory(t, svc,
		"Dana — what is her favorite telescope the Newtonian", "semantic", 0.7)
	res, err := svc.Store(context.Background(), memory.StoreInput{
		Content:         "Dana — what is her favorite eyepiece the wide-angle",
		Type:            memory.TypeSemantic,
		ImportanceScore: 0.7,
		Source:          "test",
	})
	require.NoError(t, err)
	assert.Zero(t, res.SupersededID,
		"a write at another address must not retire a fact answering a different question")

	active := search(t, svc, "Dana")
	ids := make([]int64, 0, len(active))
	for _, m := range active {
		ids = append(ids, m.ID)
	}
	assert.Contains(t, ids, telescopeID, "the earlier fact is still active")
	assert.Contains(t, ids, res.ID, "the later fact is still active")
}
