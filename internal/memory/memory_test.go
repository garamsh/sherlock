package memory_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
)

var now = time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)

// almaProject is the address most of these tests write to: a subject and
// the question a value answers about it.
var almaProject = memory.Address{Subject: "Alma", Question: "which project does she lead"}

func newService(t *testing.T, opts ...memory.Option) memory.Service {
	t.Helper()
	svc, _, _ := newServiceWithStore(t, opts...)
	return svc
}

// newServiceWithStore also hands back the store and its path, for the
// tests that must write a row the service cannot produce or read a
// column the service does not return.
func newServiceWithStore(t *testing.T, opts ...memory.Option) (memory.Service, *sqlite.Store, string) {
	t.Helper()
	if len(opts) == 0 {
		opts = []memory.Option{memory.WithClock(func() time.Time { return now })}
	}
	path := filepath.Join(t.TempDir(), "memory.db")
	st, err := sqlite.Open(path)
	require.NoError(t, err)
	svc := memory.NewService(st, opts...)
	t.Cleanup(func() { assert.NoError(t, svc.Close()) })
	return svc, st, path
}

func write(t *testing.T, svc memory.Service, a memory.Address, value string, op memory.Operation) memory.StoreResult {
	t.Helper()
	res, err := svc.Store(context.Background(), memory.StoreInput{
		Address: a, Value: value, Operation: op, Source: "test",
	})
	require.NoError(t, err)
	require.Positive(t, res.ID)
	return res
}

func valuesAt(t *testing.T, svc memory.Service, a memory.Address) []string {
	t.Helper()
	held, err := svc.At(context.Background(), a)
	require.NoError(t, err)
	return values(held)
}

func values(held []memory.Memory) []string {
	out := make([]string, 0, len(held))
	for _, m := range held {
		out = append(out, m.Value)
	}
	return out
}

func TestStore_ValueIsReadableAtTheAddressItWasWrittenTo(t *testing.T) {
	svc := newService(t)
	write(t, svc, almaProject, "amber-quarry", memory.OpJoin)

	assert.Equal(t, []string{"amber-quarry"}, valuesAt(t, svc, almaProject))
}

// The address settles identity, so a write lands where it was addressed
// and nowhere else. A question about the same subject is a different
// address and is not answered by this one.
func TestStore_AnAddressAnswersOnlyItsOwnQuestion(t *testing.T) {
	svc := newService(t)
	write(t, svc, almaProject, "amber-quarry", memory.OpJoin)

	assert.Empty(t, valuesAt(t, svc,
		memory.Address{Subject: "Alma", Question: "which team does she sit on"}))
	assert.Empty(t, valuesAt(t, svc,
		memory.Address{Subject: "Otto", Question: "which project does he lead"}))
}

// The #529 case that forced supersession on insert out ahead of this
// store: two facts about one subject share nearly every token, so a
// similarity threshold destroyed one of them. Addresses settle it without
// comparing anything.
func TestStore_TwoQuestionsAboutOneSubjectCoexist(t *testing.T) {
	svc := newService(t)
	team := memory.Address{Subject: "Alma", Question: "which team does she sit on"}
	write(t, svc, almaProject, "amber-quarry", memory.OpReplace)
	write(t, svc, team, "platform", memory.OpReplace)

	assert.Equal(t, []string{"amber-quarry"}, valuesAt(t, svc, almaProject))
	assert.Equal(t, []string{"platform"}, valuesAt(t, svc, team),
		"a replacement empties the address written and no other")
}

func TestStore_ReplaceSupersedesWhatTheAddressHeld(t *testing.T) {
	svc := newService(t)
	first := write(t, svc, almaProject, "amber-quarry", memory.OpJoin)
	res := write(t, svc, almaProject, "basalt-run", memory.OpReplace)

	assert.Equal(t, []int64{first.ID}, res.SupersededIDs)
	assert.Equal(t, []string{"basalt-run"}, valuesAt(t, svc, almaProject))
}

func TestStore_JoinLeavesWhatTheAddressHeld(t *testing.T) {
	svc := newService(t)
	write(t, svc, almaProject, "amber-quarry", memory.OpJoin)
	res := write(t, svc, almaProject, "basalt-run", memory.OpJoin)

	assert.Empty(t, res.SupersededIDs)
	assert.ElementsMatch(t, []string{"amber-quarry", "basalt-run"}, valuesAt(t, svc, almaProject))
}

// The superseded row moves to the archive naming the value that replaced
// it, which is what makes the change reversible (ADR 0002).
func TestStore_ReplaceArchivesTheOldRowNamingItsSuccessor(t *testing.T) {
	svc, _, path := newServiceWithStore(t)
	first := write(t, svc, almaProject, "amber-quarry", memory.OpJoin)
	second := write(t, svc, almaProject, "basalt-run", memory.OpReplace)

	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer db.Close()
	var value string
	var supersededBy, validTo int64
	require.NoError(t, db.QueryRow(
		`SELECT value, superseded_by, valid_to FROM memories_archive WHERE id = ?`, first.ID,
	).Scan(&value, &supersededBy, &validTo))
	assert.Equal(t, "amber-quarry", value)
	assert.Equal(t, second.ID, supersededBy)
	assert.Equal(t, now.Unix(), validTo)
}

// A statement that narrows its question asks its own question, so it gets
// its own address and no write has to choose between the two values
// (ADR 0018 §The address names the question). A read of the wider
// question finds both; a read of the narrower finds only itself.
func TestAt_ReturnsTheAddressAskedAndTheRestrictionsUnderIt(t *testing.T) {
	svc := newService(t)
	onCall := almaProject
	onCall.Restriction = "on call"
	write(t, svc, almaProject, "amber-quarry", memory.OpReplace)
	write(t, svc, onCall, "basalt-run", memory.OpReplace)

	assert.ElementsMatch(t, []string{"amber-quarry", "basalt-run"}, valuesAt(t, svc, almaProject),
		"the wider question is answered by itself and by what restricts it")
	assert.Equal(t, []string{"basalt-run"}, valuesAt(t, svc, onCall),
		"the narrower question is answered by itself alone")
}

// A restricted write never destroys the question it restricts: they are
// two addresses, so the narrowing statement has nothing to choose.
func TestStore_ARestrictedReplaceLeavesTheWiderQuestionAlone(t *testing.T) {
	svc := newService(t)
	onCall := almaProject
	onCall.Restriction = "on call"
	write(t, svc, almaProject, "amber-quarry", memory.OpReplace)
	write(t, svc, onCall, "basalt-run", memory.OpReplace)

	held, err := svc.At(context.Background(), memory.Address{
		Subject: almaProject.Subject, Question: almaProject.Question, Restriction: ""})
	require.NoError(t, err)
	assert.Contains(t, values(held), "amber-quarry")
}

// A question naming a subject and no question is answered from every
// address under that subject. Nothing bounds it and nothing orders it by
// relevance: an addressed store has no ranking to offer (ADR 0018 §What
// is not measured).
func TestUnderSubject_ReturnsEveryAddressUnderTheSubject(t *testing.T) {
	svc := newService(t)
	write(t, svc, almaProject, "amber-quarry", memory.OpReplace)
	write(t, svc, memory.Address{Subject: "Alma", Question: "which team does she sit on"},
		"platform", memory.OpReplace)
	write(t, svc, memory.Address{Subject: "Otto", Question: "which project does he lead"},
		"cinder-lake", memory.OpReplace)

	held, err := svc.UnderSubject(context.Background(), "Alma")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"amber-quarry", "platform"}, values(held))
}

func TestUnderSubject_ReturnsMoreThanFitsRatherThanChoosing(t *testing.T) {
	svc := newService(t)
	for i := range 50 {
		write(t, svc, memory.Address{Subject: "Alma", Question: fmt.Sprintf("question %02d", i)},
			fmt.Sprintf("value-%02d", i), memory.OpReplace)
	}

	held, err := svc.UnderSubject(context.Background(), "Alma")
	require.NoError(t, err)
	assert.Len(t, held, 50,
		"the store has no budget to answer inside and no ordering to trim by")
}

// Restating a fact holds it once. The address and the value are the same,
// so there is no second thing to hold — and nothing compared the two
// texts to decide that.
func TestStore_RestatingAValueAtItsAddressHoldsItOnce(t *testing.T) {
	svc := newService(t)
	first := write(t, svc, almaProject, "amber-quarry", memory.OpJoin)
	again := write(t, svc, almaProject, "amber-quarry", memory.OpJoin)

	assert.Equal(t, first.ID, again.ID)
	assert.Equal(t, []string{"amber-quarry"}, valuesAt(t, svc, almaProject))
}

// A join restating what a replacement wrote keeps the correction's
// salience: a correction never drops a tier (ADR 0006).
func TestStore_RestatingACorrectionKeepsItsSalience(t *testing.T) {
	svc := newService(t)
	write(t, svc, almaProject, "amber-quarry", memory.OpReplace)
	write(t, svc, almaProject, "amber-quarry", memory.OpJoin)

	held, err := svc.At(context.Background(), almaProject)
	require.NoError(t, err)
	require.Len(t, held, 1)
	assert.InDelta(t, memory.SalienceReplace, held[0].ImportanceScore, 1e-9)
}

// Salience is platform-derived from the operation the write states, never
// model-written (ADR 0006).
func TestStore_SalienceComesFromTheOperation(t *testing.T) {
	svc := newService(t)
	write(t, svc, almaProject, "amber-quarry", memory.OpReplace)
	write(t, svc, memory.Address{Subject: "Otto", Question: "which project does he lead"},
		"basalt-run", memory.OpJoin)

	replaced, err := svc.At(context.Background(), almaProject)
	require.NoError(t, err)
	require.Len(t, replaced, 1)
	assert.InDelta(t, memory.SalienceReplace, replaced[0].ImportanceScore, 1e-9)

	joined, err := svc.At(context.Background(),
		memory.Address{Subject: "Otto", Question: "which project does he lead"})
	require.NoError(t, err)
	require.Len(t, joined, 1)
	assert.InDelta(t, memory.SalienceJoin, joined[0].ImportanceScore, 1e-9)
}

// ADR 0018 §Forgetting is decided by use rests on a read being recorded,
// so every read records one.
func TestAt_RecordsAccess(t *testing.T) {
	svc, store, _ := newServiceWithStore(t)
	write(t, svc, almaProject, "amber-quarry", memory.OpJoin)
	_, err := svc.At(context.Background(), almaProject)
	require.NoError(t, err)

	active, err := store.Active(context.Background())
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, int64(1), active[0].AccessCount)
	assert.Equal(t, now, active[0].LastAccessed)
}

func TestStore_EvictsLowestScoreBeyondCapacity(t *testing.T) {
	svc := newService(t,
		memory.WithClock(func() time.Time { return now }),
		memory.WithCapacity(3))
	for i := range 3 {
		write(t, svc, memory.Address{Subject: "Alma", Question: fmt.Sprintf("question %d", i)},
			fmt.Sprintf("value-%d", i), memory.OpJoin)
	}
	res := write(t, svc, memory.Address{Subject: "Alma", Question: "the fourth question"},
		"value-3", memory.OpReplace)

	require.Len(t, res.EvictedIDs, 1)
	held, err := svc.UnderSubject(context.Background(), "Alma")
	require.NoError(t, err)
	assert.Len(t, held, 3, "the active index stays within capacity")
	assert.NotContains(t, values(held), "value-0",
		"the lowest-scoring memory is the one that left")
}

func TestService_RejectsInvalidInput(t *testing.T) {
	svc := newService(t)
	cases := map[string]memory.StoreInput{
		"empty subject":  {Address: memory.Address{Question: "q"}, Value: "v", Operation: memory.OpJoin},
		"empty question": {Address: memory.Address{Subject: "s"}, Value: "v", Operation: memory.OpJoin},
		"empty value":    {Address: almaProject, Operation: memory.OpJoin},
		"unknown op":     {Address: almaProject, Value: "v", Operation: memory.Operation("merge")},
		"no op":          {Address: almaProject, Value: "v"},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Store(context.Background(), in)
			assert.ErrorIs(t, err, memory.ErrInvalidInput)
		})
	}
}

// An empty restriction is not invalid: a sentence that narrows nothing
// places none, which is most of what a writeback states.
func TestStore_AcceptsAnAddressWithNoRestriction(t *testing.T) {
	svc := newService(t)
	_, err := svc.Store(context.Background(), memory.StoreInput{
		Address: almaProject, Value: "amber-quarry", Operation: memory.OpJoin, Source: "test"})
	assert.NoError(t, err)
}
