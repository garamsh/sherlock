package memory_test

import (
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/memory"
)

// retentionRecord is ADR 0006, the record the retention pass implements.
const retentionRecord = "../../docs/architecture/adr/0006-memory-lifecycle.md"

// retentionClaim reads the window out of the record's tiered-retention
// bullet. It matches the unit and the number together, because the
// defect it guards was a unit disagreement rather than a wrong value.
var retentionClaim = regexp.MustCompile(`bounded window of wall-clock time — (\d+) days`)

// TestRetentionWindow_RecordAndCodeAgree is the claim register for the
// retention window, after the registers under docs/research/: the
// record states the window a reader is bound by, this package enforces
// it, and nothing made the two agree. Issue #510 is what that costs —
// the record said the window was ~100 turns while the code counted
// seven days, `go test ./...` passed, and a reader of the record
// believed something false. Reading the record here makes a change to
// either side fail rather than drift.
func TestRetentionWindow_RecordAndCodeAgree(t *testing.T) {
	record, err := os.ReadFile(retentionRecord)
	require.NoError(t, err, "the record the retention pass implements must be readable")

	match := retentionClaim.FindSubmatch(record)
	require.NotNil(t, match,
		"ADR 0006 must state the retention window in the unit this package measures it in "+
			"(wall-clock days); issue #510 is what a mismatch costs")

	days, err := strconv.Atoi(string(match[1]))
	require.NoError(t, err)
	assert.Equal(t, memory.RetentionWindow, time.Duration(days)*24*time.Hour,
		"ADR 0006 states %d days; RetentionWindow is %s. The record and the code "+
			"state the window in the same unit, so they state the same window.",
		days, memory.RetentionWindow)
}

// TestRetentionWindow_IsWallClockFromTheTimestamp pins what the unit
// means, so the record's account of it stays checkable: the window runs
// from the memory's timestamp and is not advanced by turns. A memory
// written one second before the boundary survives a pass at the
// boundary and drops at the next pass after it, whether one turn
// happened in between or a hundred.
//
// The row is written through the store rather than the service, because
// no operation a writeback can state produces a salience inside the
// window's tier — see TestRetain_DropsNothingAWritebackWrote below.
func TestRetentionWindow_IsWallClockFromTheTimestamp(t *testing.T) {
	clock := now
	svc, store, _ := newServiceWithStore(t, memory.WithClock(func() time.Time { return clock }))
	mid, _, err := store.Write(t.Context(), memory.Memory{
		Address:         memory.Address{Subject: "Alma", Question: "what was asked"},
		Value:           "the printer again",
		ImportanceScore: (memory.RetentionKeepSalience + memory.RetentionDropSalience) / 2,
		Timestamp:       now,
		Source:          "test",
	}, nil, now)
	require.NoError(t, err)

	// A hundred retention passes inside the window drop nothing: the
	// pass counts no turns, so running it does not age the memory.
	clock = now.Add(memory.RetentionWindow - time.Second)
	for range 100 {
		archived, err := svc.Retain(t.Context())
		require.NoError(t, err)
		assert.Empty(t, archived,
			"inside the window the mid tier survives every pass, however many run")
	}

	// One second past it, the same memory drops on the first pass,
	// having been carried by no turn at all in between.
	clock = now.Add(memory.RetentionWindow + time.Second)
	archived, err := svc.Retain(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []int64{mid}, archived,
		"past the window the mid tier drops on elapsed time alone")
}

// TestRetain_DropsNothingAWritebackWrote records what the addressed
// store did to the retention pass. Salience is derived from the
// operation the write states, and both operations sit at or above
// RetentionKeepSalience, so every value the writeback stores is in the
// keep-indefinitely tier and the pass has nothing to drop.
//
// The tier it used to drop was the episodic one, and #582 removed the
// only write that carried it: an addressed store has nowhere to put a
// summary of turns it cannot address. The pass still runs and still
// drops a lower-salience row (above), so what changed is what reaches
// it, not what it does.
func TestRetain_DropsNothingAWritebackWrote(t *testing.T) {
	clock := now
	svc := newService(t, memory.WithClock(func() time.Time { return clock }))
	for _, op := range []memory.Operation{memory.OpReplace, memory.OpJoin} {
		_, err := svc.Store(t.Context(), memory.StoreInput{
			Address:   memory.Address{Subject: "Alma", Question: "which project " + string(op)},
			Value:     "amber-quarry",
			Operation: op,
			Source:    "agent",
		})
		require.NoError(t, err)
	}

	clock = now.Add(100 * memory.RetentionWindow)
	archived, err := svc.Retain(t.Context())
	require.NoError(t, err)
	assert.Empty(t, archived,
		"both salience tiers a write can state are at or above RetentionKeepSalience")
	assert.GreaterOrEqual(t, memory.SalienceJoin, memory.RetentionKeepSalience,
		"a join is in the keep tier")
	assert.GreaterOrEqual(t, memory.SalienceReplace, memory.RetentionKeepSalience,
		"a replacement is in the keep tier")
}
