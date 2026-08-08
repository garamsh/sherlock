package gateway_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
)

// openCaptureService returns a memory service over a real SQLite store in
// dir, plus the store itself for row-level assertions.
func openCaptureService(t *testing.T, dir string) (memory.Service, memory.Store) {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(dir, "memory.db"))
	require.NoError(t, err)
	svc := memory.NewService(store, memory.NewHashingEmbedder(), memory.NewConcatSummarizer())
	t.Cleanup(func() {
		assert.NoError(t, svc.Close())
	})
	return svc, store
}

func TestCapture_StoresVerbatimUserTurn(t *testing.T) {
	svc, store := openCaptureService(t, t.TempDir())
	capture := gateway.Capture{Svc: svc}

	err := capture.Capture(context.Background(), "remember that the deploy key is blue")
	require.NoError(t, err)

	active, err := store.Active(context.Background())
	require.NoError(t, err)
	require.Len(t, active, 1)
	got := active[0]
	assert.Equal(t, "remember that the deploy key is blue", got.Content,
		"the message is stored verbatim")
	assert.Equal(t, memory.TypeEpisodic, got.Type)
	assert.Equal(t, gateway.SourceUserTurn, got.Source)
	assert.Equal(t, gateway.CapturedTurnImportance, got.ImportanceScore)
	assert.Less(t, got.ImportanceScore, gateway.DefaultAddImportance,
		"raw turns rank below curated writeback facts")
	assert.NotZero(t, got.Timestamp)
}

func TestCapture_EmptyMessageRejected(t *testing.T) {
	svc, store := openCaptureService(t, t.TempDir())
	capture := gateway.Capture{Svc: svc}

	err := capture.Capture(context.Background(), "")
	assert.ErrorIs(t, err, memory.ErrInvalidInput)

	active, err := store.Active(context.Background())
	require.NoError(t, err)
	assert.Empty(t, active, "nothing is stored for an empty message")
}
