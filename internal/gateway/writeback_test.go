package gateway_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/memory"
)

// telescope is the address a user's stated preference lands on.
func telescope() agent.MemoryAddress {
	return agent.MemoryAddress{
		Subject:  "user",
		Question: "what is the user's favorite telescope",
	}
}

func TestWriteback_StoresTheAddressWithItsValue(t *testing.T) {
	svc, _ := newMemoryService(t)
	wb := gateway.Writeback{Svc: svc}

	_, err := wb.Apply(context.Background(), []agent.WritebackOp{{
		Address: agent.MemoryAddress{
			Subject:  "user",
			Question: "which colour scheme does the user prefer",
		},
		Value:     "dark mode",
		Operation: agent.OpJoin,
	}})
	require.NoError(t, err)

	found, err := svc.Search(context.Background(), "colour scheme dark mode", 1)
	require.NoError(t, err)
	require.NotEmpty(t, found)
	assert.Contains(t, found[0].Content, "user")
	assert.Contains(t, found[0].Content, "which colour scheme does the user prefer")
	assert.Contains(t, found[0].Content, "dark mode")
	assert.Equal(t, memory.TypeSemantic, found[0].Type,
		"a writeback states a fact, never an observation of the turn")
}

// TestWriteback_RestrictionReachesTheStore is ADR 0018 §The address
// names the question: a statement that narrows its question narrows its
// address, so the narrowing is part of what is written rather than
// something a later read has to infer.
//
// Whether the two values then coexist is the store's business and not
// this step's: the shipped store still supersedes on insert (#336 step 2
// removes it), and two sentences this alike are what that mechanism
// destroys.
func TestWriteback_RestrictionReachesTheStore(t *testing.T) {
	svc, _ := newMemoryService(t)
	ctx := context.Background()
	wb := gateway.Writeback{Svc: svc}

	narrowed := telescope()
	narrowed.Restriction = "for planets"
	_, err := wb.Apply(ctx, []agent.WritebackOp{
		{Address: narrowed, Value: "the Orion XT8", Operation: agent.OpJoin},
	})
	require.NoError(t, err)

	found, err := svc.Search(ctx, "favorite telescope planets", 1)
	require.NoError(t, err)
	require.NotEmpty(t, found)
	assert.True(t,
		containsAll(found[0].Content, "for planets", "the Orion XT8",
			"what is the user's favorite telescope"),
		"the stored sentence carries the whole address: %q", found[0].Content)
}

// TestWriteback_SalienceFollowsTheOperation pins ADR 0006's rule that
// salience is platform-derived, never model-written: a replacement is a
// correction and ranks above a value that joins what is held.
func TestWriteback_SalienceFollowsTheOperation(t *testing.T) {
	tests := []struct {
		name         string
		operation    agent.MemoryOperation
		wantSalience float64
	}{
		{"a replacement is a correction", agent.OpReplace, gateway.SalienceUpdate},
		{"a join adds a fact", agent.OpJoin, gateway.SalienceAddSemantic},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newMemoryService(t)
			wb := gateway.Writeback{Svc: svc}

			_, err := wb.Apply(context.Background(), []agent.WritebackOp{{
				Address:   telescope(),
				Value:     "the Kepler-9 refractor",
				Operation: tt.operation,
			}})
			require.NoError(t, err)

			found, err := svc.Search(context.Background(), "favorite telescope", 1)
			require.NoError(t, err)
			require.NotEmpty(t, found)
			assert.InDelta(t, tt.wantSalience, found[0].ImportanceScore, 0.0001)
		})
	}
}

// TestWriteback_SurfacesSimilarMemories is the ADR 0006 step 5
// acceptance case: writing a fact that contradicts an existing memory
// surfaces the existing memory for judgment — and not the just-written
// version itself.
func TestWriteback_SurfacesSimilarMemories(t *testing.T) {
	svc, _ := newMemoryService(t)
	ctx := context.Background()
	// Seeded in the sentence form the store held before this change, and
	// deliberately below SupersessionSimilarity: the coherence note is the
	// backstop for pairs insert-time supersession does not catch (ADR 0006).
	const seeded = "user's favorite telescope is the Kepler-9 refractor"
	stored, err := svc.Store(ctx, memory.StoreInput{
		Content:         seeded,
		Type:            memory.TypeSemantic,
		ImportanceScore: 0.5,
		Source:          "agent",
	})
	require.NoError(t, err)

	wb := gateway.Writeback{Svc: svc}
	notes, err := wb.Apply(ctx, []agent.WritebackOp{{
		Address: agent.MemoryAddress{
			Subject:  "user",
			Question: "which telescope does the user use for planets",
		},
		Value:     "the Orion XT8",
		Operation: agent.OpJoin,
	}})
	require.NoError(t, err)

	require.NotEmpty(t, notes, "the contradicting memory is presented for judgment")
	var found bool
	for _, n := range notes {
		if n.Content == seeded {
			found = true
			assert.Equal(t, strconv.FormatInt(stored.ID, 10), n.ID)
			assert.NotEmpty(t, n.Age)
		}
		assert.NotContains(t, n.Content, "Orion XT8",
			"the just-written version never judges itself")
	}
	assert.True(t, found, "the seeded memory is in the coherence set: %v", notes)
}

// TestWriteback_EmptyWritebackRetrievesNothing pins the coherence scope
// (ADR 0006 step 5): a turn that states no fact asks the store nothing.
func TestWriteback_EmptyWritebackRetrievesNothing(t *testing.T) {
	svc, _ := newMemoryService(t)
	wb := gateway.Writeback{Svc: svc}

	notes, err := wb.Apply(context.Background(), []agent.WritebackOp{})
	require.NoError(t, err)
	assert.Empty(t, notes)
}

// containsAll reports whether s contains every substring.
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
