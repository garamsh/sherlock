//go:build unix

package sqlite_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

// TestOpenConversation_RefusesSecondWriter holds the one-writer rule:
// while one opener holds a store, every further opener of it is refused,
// under any spelling of its path. The control opens a second store in the same
// directory at the same time, so the refusal is the lock on this store
// and not an opener that fails on anything held open beside it.
func TestOpenConversation_RefusesSecondWriter(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "real")
	require.NoError(t, os.Mkdir(dir, 0o755))
	require.NoError(t, os.Symlink(dir, filepath.Join(root, "alias")))
	path := filepath.Join(dir, "memory.db")

	first, err := sqlite.OpenConversation(path, nil)
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

			_, err = sqlite.OpenConversation(tt.path, nil)
			assert.ErrorIs(t, err, sqlite.ErrStoreLocked)
		})
	}

	other, err := sqlite.OpenConversation(filepath.Join(dir, "other.db"), nil)
	require.NoError(t, err, "the control: another store beside the held one opens")
	assert.NoError(t, other.Close())
}

// TestOpenConversation_LocksBeforeOpeningTheFile holds the order: the
// lock is taken before memory.db is opened, so a refused opener neither
// creates the file nor reads it. Each control is the same opener once the holder
// closes, which does both.
func TestOpenConversation_LocksBeforeOpeningTheFile(t *testing.T) {
	t.Run("absent file is not created", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "memory.db")
		first, err := sqlite.OpenConversation(path, nil)
		require.NoError(t, err)
		require.NoError(t, os.Remove(path))
		require.NoFileExists(t, path, "the setup: the store's file is gone while its lock is held")

		_, err = sqlite.OpenConversation(path, nil)
		require.ErrorIs(t, err, sqlite.ErrStoreLocked)
		assert.NoFileExists(t, path)

		require.NoError(t, first.Close())
		reopened, err := sqlite.OpenConversation(path, nil)
		require.NoError(t, err)
		assert.FileExists(t, path, "the control: an opener holding the lock creates the file")
		assert.NoError(t, reopened.Close())
	})
	t.Run("present file is not inspected", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "memory.db")
		first, err := sqlite.OpenConversation(path, nil)
		require.NoError(t, err)
		raw, err := sql.Open("sqlite", path)
		require.NoError(t, err)
		_, err = raw.Exec(`CREATE TABLE memories_active (id INTEGER PRIMARY KEY); INSERT INTO memories_active(id) VALUES(7)`)
		require.NoError(t, err)
		require.NoError(t, raw.Close())

		_, err = sqlite.OpenConversation(path, nil)
		assert.ErrorIs(t, err, sqlite.ErrStoreLocked)

		require.NoError(t, first.Close())
		_, err = sqlite.OpenConversation(path, nil)
		assert.ErrorIs(t, err, sqlite.ErrAddressedStore, "the control: unheld, the same file is refused for its addressed row")
	})
}

// TestOpenConversation_ReopensAfterClose holds the release: closing the
// holder frees the store for the next opener.
func TestOpenConversation_ReopensAfterClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	first, err := sqlite.OpenConversation(path, nil)
	require.NoError(t, err)
	_, err = sqlite.OpenConversation(path, nil)
	require.ErrorIs(t, err, sqlite.ErrStoreLocked, "the setup: the store is held")

	require.NoError(t, first.Close())
	second, err := sqlite.OpenConversation(path, nil)
	require.NoError(t, err)
	assert.NoError(t, second.Close())
}

// TestOpenInitializedConversation_ChecksUnderTheWriterLock holds the
// check's place after the writer lock (ADR 0025): while another process
// holds the lock of an absent store, the opener is refused for the lock
// and never reaches the check. The control releases the lock, and the
// same opener then reaches the check and refuses the absent store.
func TestOpenInitializedConversation_ChecksUnderTheWriterLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	holder, err := os.OpenFile(path+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	require.NoError(t, err)
	require.NoError(t, unix.Flock(int(holder.Fd()), unix.LOCK_EX|unix.LOCK_NB))

	_, err = sqlite.OpenInitializedConversation(path, nil)
	require.ErrorIs(t, err, sqlite.ErrStoreLocked)
	assert.NotErrorIs(t, err, sqlite.ErrStoreUninitialized)

	require.NoError(t, holder.Close())
	_, err = sqlite.OpenInitializedConversation(path, nil)
	assert.ErrorIs(t, err, sqlite.ErrStoreUninitialized, "control: the released lock reaches the check")
	assert.NoFileExists(t, path)
}
