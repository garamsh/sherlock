//go:build linux

package files_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/files"
	"github.com/garamsh/gagent/internal/shell"
)

// TestFS_PrivilegedWritesAsAgentUID pins the agent-UID workspace domain
// (ADR 0008, ADR 0010): a privileged files tool drops its filesystem UID, so a file
// it writes is owned by the agent UID like a shell-created file.
func TestFS_PrivilegedWritesAsAgentUID(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("setfsuid path requires root; run with `sudo -E go test ./internal/files/...` to exercise it")
	}
	root := t.TempDir()
	require.NoError(t, os.Chown(root, shell.DefaultAgentUID, -1))

	fs, err := files.Open(root, shell.DefaultAgentUID)
	require.NoError(t, err)
	require.NoError(t, fs.Write("a.txt", []byte("data")))

	info, err := os.Stat(filepath.Join(root, "a.txt"))
	require.NoError(t, err)
	stat, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	assert.Equal(t, uint32(shell.DefaultAgentUID), stat.Uid,
		"a files-tool write is owned by the agent UID")

	content, err := fs.Read("a.txt")
	require.NoError(t, err)
	assert.Equal(t, "data", string(content))
}

// TestFS_UnprivilegedKeepsCurrentUID pins the dev contract: an
// unprivileged caller attempts no setfsuid and owns written files itself.
func TestFS_UnprivilegedKeepsCurrentUID(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("this case is only meaningful when the platform cannot setfsuid")
	}
	root := t.TempDir()
	fs, err := files.Open(root, shell.DefaultAgentUID)
	require.NoError(t, err)
	require.NoError(t, fs.Write("a.txt", []byte("data")))

	info, err := os.Stat(filepath.Join(root, "a.txt"))
	require.NoError(t, err)
	stat, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	assert.Equal(t, uint32(os.Geteuid()), stat.Uid,
		"no setfsuid attempted; the platform owns the file itself")
}
