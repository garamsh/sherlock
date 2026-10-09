//go:build unix

package sqlite_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

// storeFiles names memory.db, the two sidecars SQLite keeps beside it in
// WAL mode, and the writer lock.
func storeFiles(path string) []string {
	return []string{path, path + "-wal", path + "-shm", path + ".lock"}
}

// recordOne commits one inbound message, so the open store holds a -wal
// and a -shm as well as memory.db.
func recordOne(t *testing.T, store *sqlite.Store) {
	t.Helper()
	require.NoError(t, store.RecordTurn(context.Background(),
		[]agent.SourceMessage{{Role: agent.RoleUser, ExternalID: "m1", Sender: "s", Content: "remember cedar"}}, nil))
}

// assertOwnerOnly asserts each file exists with mode 0600.
func assertOwnerOnly(t *testing.T, files []string) {
	t.Helper()
	for _, name := range files {
		info, err := os.Stat(name)
		require.NoError(t, err, "the setup: %s exists", filepath.Base(name))
		assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm(), "%s", filepath.Base(name))
	}
}

// TestOpenConversation_CreatesTheStoreOwnerOnly holds that a new store's
// files, including the sidecars SQLite creates on its own, are owner-only
// under the process's umask rather than at SQLite's default 0644 (#998).
func TestOpenConversation_CreatesTheStoreOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.OpenConversation(path, nil)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, store.Close()) })
	recordOne(t, store)

	assertOwnerOnly(t, storeFiles(path))
}

// TestOpenConversation_TightensAWiderStoreWithoutChangingIt holds the
// choice for a store an earlier release left at 0644: the next start
// tightens every file to owner-only and still reads what the store held.
// The copy is taken from an open store, so its -wal and -shm exist before
// the start that tightens them.
func TestOpenConversation_TightensAWiderStoreWithoutChangingIt(t *testing.T) {
	source := filepath.Join(t.TempDir(), "memory.db")
	original, err := sqlite.OpenConversation(source, nil)
	require.NoError(t, err)
	recordOne(t, original)
	path := filepath.Join(t.TempDir(), "memory.db")
	for i, name := range storeFiles(source) {
		body, err := os.ReadFile(name)
		require.NoError(t, err)
		copied := storeFiles(path)[i]
		require.NoError(t, os.WriteFile(copied, body, 0o644))
		require.NoError(t, os.Chmod(copied, 0o644))
	}
	require.NoError(t, original.Close())
	for _, name := range storeFiles(path) {
		info, err := os.Stat(name)
		require.NoError(t, err)
		require.Equal(t, fs.FileMode(0o644), info.Mode().Perm(), "the setup: %s starts wider", filepath.Base(name))
	}

	store, err := sqlite.OpenConversation(path, nil)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, store.Close()) })

	assertOwnerOnly(t, storeFiles(path))
	held, same, err := store.HoldsInbound(context.Background(), "", "s", "m1", "remember cedar")
	require.NoError(t, err)
	assert.True(t, held && same, "the tightened store lost the message it held")
}
