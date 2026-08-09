package files_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/files"
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
