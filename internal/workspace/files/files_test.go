package files_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/workspace/files"
)

func TestFS_TraversalRejected(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "parent escape", path: "../escape.txt"},
		{name: "bare parent", path: ".."},
		{name: "deep escape", path: "../../etc/passwd"},
		{name: "absolute path", path: "/etc/passwd"},
		{name: "subdir then escape", path: "sub/../../escape.txt"},
		{name: "root escape", path: "./../../.."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs, err := files.Open(t.TempDir())
			require.NoError(t, err)

			_, readErr := fs.Read(tt.path)
			assert.ErrorIs(t, readErr, files.ErrOutsideWorkspace)

			writeErr := fs.Write(tt.path, []byte("x"))
			assert.ErrorIs(t, writeErr, files.ErrOutsideWorkspace)

			_, listErr := fs.List(tt.path)
			assert.ErrorIs(t, listErr, files.ErrOutsideWorkspace)
		})
	}
}

func TestFS_TraversalDoesNotTouchOutsideFiles(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "outside-marker.txt")
	require.NoError(t, os.WriteFile(outside, []byte("untouched"), 0o644))

	fs, err := files.Open(root)
	require.NoError(t, err)
	err = fs.Write("../outside-marker.txt", []byte("overwritten"))

	assert.ErrorIs(t, err, files.ErrOutsideWorkspace)
	content, readErr := os.ReadFile(outside)
	require.NoError(t, readErr)
	assert.Equal(t, "untouched", string(content))
}

func TestFS_AllowedPaths(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "plain file", path: "a.txt"},
		{name: "nested file", path: "sub/dir/b.txt"},
		{name: "dot prefix", path: "./a.txt"},
		{name: "resolved parent inside root", path: "sub/../a.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs, err := files.Open(t.TempDir())
			require.NoError(t, err)

			require.NoError(t, fs.Write(tt.path, []byte("data")))
			content, err := fs.Read(tt.path)
			require.NoError(t, err)
			assert.Equal(t, "data", string(content))
		})
	}
}

func TestFS_WriteReadListRoundtrip(t *testing.T) {
	fs, err := files.Open(t.TempDir())
	require.NoError(t, err)

	require.NoError(t, fs.Write("notes/todo.txt", []byte("buy milk")))
	require.NoError(t, fs.Write("top.txt", []byte("root file")))

	content, err := fs.Read("notes/todo.txt")
	require.NoError(t, err)
	assert.Equal(t, "buy milk", string(content))

	entries, err := fs.List("")
	require.NoError(t, err)
	assert.Equal(t, []files.Entry{
		{Name: "notes", Dir: true},
		{Name: "top.txt", Dir: false},
	}, entries)
}

func TestFS_ReadMissingFile(t *testing.T) {
	fs, err := files.Open(t.TempDir())
	require.NoError(t, err)

	_, err = fs.Read("nope.txt")

	require.Error(t, err)
	assert.NotErrorIs(t, err, files.ErrOutsideWorkspace)
}

// TestFS_HandOverUnprivilegedServerSkipsChown pins the single-account
// posture: a server that is not root owns the workspace directory
// itself, so handing it to the exec UID is a no-op and the directory
// keeps the mode a sole owner needs. The privileged path needs euid 0
// and is exercised by deployments, not unit tests.
func TestFS_HandOverUnprivilegedServerSkipsChown(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("test asserts the unprivileged no-op path")
	}
	root := t.TempDir()
	fs, err := files.Open(root)
	require.NoError(t, err)
	before, err := os.Stat(root)
	require.NoError(t, err)

	require.NoError(t, fs.HandOver(1001))

	after, err := os.Stat(root)
	require.NoError(t, err)
	assert.Equal(t, before.Mode().Perm(), after.Mode().Perm(),
		"a workspace the process owns keeps its mode: no owner to share the directory with")
}

// TestFS_WriteReplacesAFileItCannotOpen pins the property the exec
// account needs from the gRPC write path: the file it replaces may
// belong to another account, so the write goes through a rename, which
// needs the directory's write bit rather than the file's. An unwritable
// file stands in for the exec account's here — the ownership case needs
// two accounts and is exercised by deployments.
func TestFS_WriteReplacesAFileItCannotOpen(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root opens a 0444 file for writing, so the case cannot be staged")
	}
	root := t.TempDir()
	fs, err := files.Open(root)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.txt"), []byte("first\n"), 0o444))

	require.NoError(t, fs.Write("note.txt", []byte("second\n")))

	content, err := fs.Read("note.txt")
	require.NoError(t, err)
	assert.Equal(t, "second\n", string(content))
	info, err := os.Stat(filepath.Join(root, "note.txt"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm(),
		"the replacement carries the documented file mode")
}

// TestFS_WriteCreatesParentsAtTheDirectoryMode pins what a nested write
// leaves behind: the parents it creates carry the same mode as the
// workspace root, so a directory the gRPC contract created is one exec'd
// children can write in too.
func TestFS_WriteCreatesParentsAtTheDirectoryMode(t *testing.T) {
	root := t.TempDir()
	fs, err := files.Open(root)
	require.NoError(t, err)

	require.NoError(t, fs.Write(filepath.Join("sub", "deeper", "note.txt"), []byte("hi\n")))

	for _, dir := range []string{"sub", filepath.Join("sub", "deeper")} {
		info, err := os.Stat(filepath.Join(root, dir))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o755), info.Mode().Perm(), dir)
	}
}
