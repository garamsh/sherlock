package gateway_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/memory"
)

func TestWriteback_AddStoresNewMemory(t *testing.T) {
	svc, _ := newMemoryService(t)
	wb := gateway.Writeback{Svc: svc}

	err := wb.Apply(context.Background(), []agent.WritebackOp{
		{Kind: agent.OpAdd, Content: "user prefers dark mode", Type: "semantic"},
	})
	require.NoError(t, err)

	found, err := svc.Search(context.Background(), "dark mode", 1)
	require.NoError(t, err)
	require.NotEmpty(t, found)
	assert.Equal(t, "user prefers dark mode", found[0].Content)
	assert.Equal(t, memory.TypeSemantic, found[0].Type)
}

func TestWriteback_AddWithoutTypeDefaultsToEpisodic(t *testing.T) {
	svc, _ := newMemoryService(t)
	wb := gateway.Writeback{Svc: svc}

	err := wb.Apply(context.Background(), []agent.WritebackOp{
		{Kind: agent.OpAdd, Content: "asked about the weather"},
	})
	require.NoError(t, err)

	found, err := svc.Search(context.Background(), "weather", 1)
	require.NoError(t, err)
	require.NotEmpty(t, found)
	assert.Equal(t, memory.TypeEpisodic, found[0].Type)
}

func TestWriteback_UpdateSupersedesMemory(t *testing.T) {
	svc, store := newMemoryService(t)
	ctx := context.Background()
	stored, err := svc.Store(ctx, memory.StoreInput{
		Content:         "old fact",
		Type:            memory.TypeSemantic,
		ImportanceScore: 0.5,
		Source:          "agent",
	})
	require.NoError(t, err)

	wb := gateway.Writeback{Svc: svc}
	err = wb.Apply(ctx, []agent.WritebackOp{
		{Kind: agent.OpUpdate, ID: strconv.FormatInt(stored.ID, 10), Content: "new fact"},
	})
	require.NoError(t, err)

	_, err = store.Get(ctx, stored.ID)
	assert.ErrorIs(t, err, memory.ErrNotFound)
	found, err := svc.Search(ctx, "new fact", 1)
	require.NoError(t, err)
	require.NotEmpty(t, found)
	assert.Equal(t, "new fact", found[0].Content)
}

func TestWriteback_DeleteArchivesMemory(t *testing.T) {
	svc, store := newMemoryService(t)
	ctx := context.Background()
	stored, err := svc.Store(ctx, memory.StoreInput{
		Content:         "temporary note",
		Type:            memory.TypeEpisodic,
		ImportanceScore: 0.5,
		Source:          "agent",
	})
	require.NoError(t, err)

	wb := gateway.Writeback{Svc: svc}
	err = wb.Apply(ctx, []agent.WritebackOp{
		{Kind: agent.OpDelete, ID: strconv.FormatInt(stored.ID, 10)},
	})
	require.NoError(t, err)

	_, err = store.Get(ctx, stored.ID)
	assert.ErrorIs(t, err, memory.ErrNotFound)
}

func TestWriteback_KeepIsANoOp(t *testing.T) {
	svc, _ := newMemoryService(t)
	wb := gateway.Writeback{Svc: svc}

	err := wb.Apply(context.Background(), []agent.WritebackOp{
		{Kind: agent.OpKeep, ID: "42"},
	})
	assert.NoError(t, err)
}

func TestWriteback_InvalidIDFails(t *testing.T) {
	svc, _ := newMemoryService(t)
	wb := gateway.Writeback{Svc: svc}

	err := wb.Apply(context.Background(), []agent.WritebackOp{
		{Kind: agent.OpDelete, ID: "not-a-number"},
	})
	assert.Error(t, err)
}

func TestWriteback_UnknownKindFails(t *testing.T) {
	svc, _ := newMemoryService(t)
	wb := gateway.Writeback{Svc: svc}

	err := wb.Apply(context.Background(), []agent.WritebackOp{
		{Kind: agent.OpKind("explode"), ID: "1"},
	})
	assert.Error(t, err)
}
