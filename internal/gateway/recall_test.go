package gateway_test

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
)

// newMemoryService returns a real memory service over a SQLite store in a
// temp dir, so adapter tests exercise the actual storage boundary.
func newMemoryService(t *testing.T) (memory.Service, memory.Store) {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "memory.db"))
	require.NoError(t, err)
	svc := memory.NewService(store, memory.NewHashingEmbedder(), memory.NewConcatSummarizer())
	t.Cleanup(func() { svc.Close() })
	return svc, store
}

func TestRecall_ReturnsStoredMemoriesWithIDAndAge(t *testing.T) {
	svc, _ := newMemoryService(t)
	ctx := context.Background()

	stored, err := svc.Store(ctx, memory.StoreInput{
		Content:         "the deploy key lives in the vault",
		Type:            memory.TypeSemantic,
		ImportanceScore: 0.8,
		Source:          "agent",
	})
	require.NoError(t, err)

	recaller := gateway.Recall{Svc: svc, Now: func() time.Time { return time.Now() }}
	recalled, err := recaller.Recall(ctx, "where is the deploy key")
	require.NoError(t, err)
	require.NotEmpty(t, recalled)
	assert.Equal(t, strconv.FormatInt(stored.ID, 10), recalled[0].ID)
	assert.Equal(t, "the deploy key lives in the vault", recalled[0].Content)
	assert.NotEmpty(t, recalled[0].Age)
}

func TestRecall_EmptyStoreReturnsNothing(t *testing.T) {
	svc, _ := newMemoryService(t)
	recaller := gateway.Recall{Svc: svc}
	recalled, err := recaller.Recall(context.Background(), "anything")
	require.NoError(t, err)
	assert.Empty(t, recalled)
}
