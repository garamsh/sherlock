//go:build unix

package sqlite_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

// TestOpen_RefusesSecondWriter holds the one-writer rule: while one
// opener holds a store, every further opener of it is refused, under any
// spelling of its path. The control opens a second store in the same
// directory at the same time, so the refusal is the lock on this store
// and not an opener that fails on anything held open beside it.
func TestOpen_RefusesSecondWriter(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "real")
	require.NoError(t, os.Mkdir(dir, 0o755))
	require.NoError(t, os.Symlink(dir, filepath.Join(root, "alias")))
	path := filepath.Join(dir, "memory.db")

	first, err := sqlite.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, first.Close()) })
	require.NoError(t, os.Symlink(path, filepath.Join(root, "linked.db")))
	held, err := os.Stat(path)
	require.NoError(t, err)

	t.Chdir(root)
	tests := []struct {
		name string
		path string
	}{
		{"same path", path},
		{"symlinked directory", filepath.Join(root, "alias", "memory.db")},
		{"symlinked file", filepath.Join(root, "linked.db")},
		// Concatenated, because filepath.Join would clean the `..` away.
		{"dot-dot", dir + "/../real/memory.db"},
		{"relative", filepath.Join("real", "memory.db")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alias, err := os.Stat(tt.path)
			require.NoError(t, err)
			require.True(t, os.SameFile(held, alias), "the setup: %q spells the held store", tt.path)

			_, err = sqlite.Open(tt.path)
			assert.ErrorIs(t, err, sqlite.ErrStoreLocked)
			_, err = sqlite.OpenConversation(tt.path, nil)
			assert.ErrorIs(t, err, sqlite.ErrStoreLocked)
		})
	}

	other, err := sqlite.Open(filepath.Join(dir, "other.db"))
	require.NoError(t, err, "the control: another store beside the held one opens")
	assert.NoError(t, other.Close())
}

// TestOpen_LocksBeforeOpeningTheFile holds the order: the lock is taken
// before memory.db is opened, so a refused opener neither creates the
// file nor reads it. Each control is the same opener once the holder
// closes, which does both.
func TestOpen_LocksBeforeOpeningTheFile(t *testing.T) {
	t.Run("absent file is not created", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "memory.db")
		first, err := sqlite.Open(path)
		require.NoError(t, err)
		require.NoError(t, os.Remove(path))
		require.NoFileExists(t, path, "the setup: the store's file is gone while its lock is held")

		_, err = sqlite.Open(path)
		require.ErrorIs(t, err, sqlite.ErrStoreLocked)
		assert.NoFileExists(t, path)

		require.NoError(t, first.Close())
		reopened, err := sqlite.Open(path)
		require.NoError(t, err)
		assert.FileExists(t, path, "the control: an opener holding the lock creates the file")
		assert.NoError(t, reopened.Close())
	})
	t.Run("present file is not inspected", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "memory.db")
		first, err := sqlite.Open(path)
		require.NoError(t, err)
		writeOne(t, first, sample(project, "Apollo"))

		_, err = sqlite.OpenConversation(path, nil)
		assert.ErrorIs(t, err, sqlite.ErrStoreLocked)

		require.NoError(t, first.Close())
		_, err = sqlite.OpenConversation(path, nil)
		assert.ErrorIs(t, err, sqlite.ErrAddressedStore, "the control: unheld, the same file is refused for its addressed row")
	})
}

// TestOpen_ReopensAfterClose holds the release: closing the holder frees
// the store for the next opener.
func TestOpen_ReopensAfterClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	first, err := sqlite.Open(path)
	require.NoError(t, err)
	_, err = sqlite.Open(path)
	require.ErrorIs(t, err, sqlite.ErrStoreLocked, "the setup: the store is held")

	require.NoError(t, first.Close())
	second, err := sqlite.OpenConversation(path, nil)
	require.NoError(t, err)
	assert.NoError(t, second.Close())
}
