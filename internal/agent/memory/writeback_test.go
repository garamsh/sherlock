package memory_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	agentmemory "github.com/garamsh/gagent/internal/agent/memory"
	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
)

// telescope is the address a user's stated preference lands on, in the
// agent's vocabulary and in the store's.
func telescope() agent.MemoryAddress {
	return agent.MemoryAddress{
		Subject:  "user",
		Question: "what is the user's favorite telescope",
	}
}

func telescopeAddress() memory.Address {
	return memory.Address{
		Subject:  "user",
		Question: "what is the user's favorite telescope",
	}
}

func newMemoryService(t *testing.T) memory.Service {
	t.Helper()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "memory.db"))
	require.NoError(t, err)
	svc := memory.NewService(st)
	t.Cleanup(func() { assert.NoError(t, svc.Close()) })
	return svc
}

func valuesAt(t *testing.T, svc memory.Service, a memory.Address) []string {
	t.Helper()
	held, err := svc.At(context.Background(), a)
	require.NoError(t, err)
	out := make([]string, 0, len(held))
	for _, m := range held {
		out = append(out, m.Value)
	}
	return out
}

func TestWriteback_StoresTheValueAtItsAddress(t *testing.T) {
	svc := newMemoryService(t)
	wb := agentmemory.Writeback{Svc: svc}
	scheme := memory.Address{Subject: "user", Question: "which colour scheme does the user prefer"}

	err := wb.Apply(context.Background(), []agent.WritebackOp{{
		Address: agent.MemoryAddress{
			Subject:  scheme.Subject,
			Question: scheme.Question,
		},
		Value:     "dark mode",
		Operation: agent.OpJoin,
	}})
	require.NoError(t, err)

	assert.Equal(t, []string{"dark mode"}, valuesAt(t, svc, scheme))
}

// TestWriteback_RestrictionReachesTheStore is ADR 0018 §The address
// names the question: a statement that narrows its question narrows its
// address, so the narrowing is part of what is written rather than
// something a later read has to infer.
//
// The two values then coexist because they are two addresses. Nothing
// compared the sentences to decide that.
func TestWriteback_RestrictionReachesTheStore(t *testing.T) {
	svc := newMemoryService(t)
	ctx := context.Background()
	wb := agentmemory.Writeback{Svc: svc}

	narrowed := telescope()
	narrowed.Restriction = "for planets"
	err := wb.Apply(ctx, []agent.WritebackOp{
		{Address: telescope(), Value: "the Kepler-9 refractor", Operation: agent.OpReplace},
		{Address: narrowed, Value: "the Orion XT8", Operation: agent.OpReplace},
	})
	require.NoError(t, err)

	restricted := telescopeAddress()
	restricted.Restriction = "for planets"
	assert.Equal(t, []string{"the Orion XT8"}, valuesAt(t, svc, restricted),
		"the restriction is part of the address written")
	assert.ElementsMatch(t,
		[]string{"the Kepler-9 refractor", "the Orion XT8"}, valuesAt(t, svc, telescopeAddress()),
		"the wider question is answered by itself and by what restricts it")
}

// TestWriteback_OperationReachesTheStore pins what the declaration buys:
// a replacement supersedes what the address held and a join leaves it.
func TestWriteback_OperationReachesTheStore(t *testing.T) {
	tests := []struct {
		name      string
		operation agent.MemoryOperation
		want      []string
	}{
		{"a replacement supersedes what is held", agent.OpReplace, []string{"the Orion XT8"}},
		{"a join leaves it", agent.OpJoin, []string{"the Kepler-9 refractor", "the Orion XT8"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newMemoryService(t)
			wb := agentmemory.Writeback{Svc: svc}

			err := wb.Apply(context.Background(), []agent.WritebackOp{
				{Address: telescope(), Value: "the Kepler-9 refractor", Operation: agent.OpJoin},
				{Address: telescope(), Value: "the Orion XT8", Operation: tt.operation},
			})
			require.NoError(t, err)

			assert.ElementsMatch(t, tt.want, valuesAt(t, svc, telescopeAddress()))
		})
	}
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
		{"a replacement is a correction", agent.OpReplace, memory.SalienceReplace},
		{"a join adds a fact", agent.OpJoin, memory.SalienceJoin},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newMemoryService(t)
			wb := agentmemory.Writeback{Svc: svc}

			err := wb.Apply(context.Background(), []agent.WritebackOp{{
				Address:   telescope(),
				Value:     "the Kepler-9 refractor",
				Operation: tt.operation,
			}})
			require.NoError(t, err)

			held, err := svc.At(context.Background(), telescopeAddress())
			require.NoError(t, err)
			require.Len(t, held, 1)
			assert.InDelta(t, tt.wantSalience, held[0].ImportanceScore, 0.0001)
		})
	}
}

// / TestWriteback_EmptyWritebackStoresNothing: a turn that states no fact
// asks the store nothing.
func TestWriteback_EmptyWritebackStoresNothing(t *testing.T) {
	svc := newMemoryService(t)
	wb := agentmemory.Writeback{Svc: svc}

	err := wb.Apply(context.Background(), []agent.WritebackOp{})
	require.NoError(t, err)
	assert.Empty(t, valuesAt(t, svc, telescopeAddress()))
}
