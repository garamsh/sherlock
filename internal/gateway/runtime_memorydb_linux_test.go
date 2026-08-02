//go:build linux

package gateway_test

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/shell"
)

// TestFactory_PrivilegedMemoryDBOwnedByMemoryUID pins the ADR 0006
// memory-UID domain: a privileged platform chowns memory.db to the
// memory UID with mode 0600 at environment creation, and an agent-UID
// child gets EACCES from the kernel reading it.
func TestFactory_PrivilegedMemoryDBOwnedByMemoryUID(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("chown path requires root; run with `sudo -E go test ./internal/gateway/...` to exercise it")
	}
	root := t.TempDir()
	// t.TempDir is 0700; the agent-UID child must traverse down to the
	// environment directory so the EACCES comes from memory.db itself.
	require.NoError(t, os.Chmod(root, 0o755))
	factory := &gateway.Factory{
		Root:     root,
		NewModel: replyModel,
	}
	rt, err := factory.New(context.Background(), "env-1", nil)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, rt.Close(context.Background()))
	}()

	dbPath := filepath.Join(root, "env-1", gateway.MemoryDBFile)
	info, err := os.Stat(dbPath)
	require.NoError(t, err)
	stat, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	assert.Equal(t, uint32(shell.DefaultMemoryUID), stat.Uid,
		"memory.db is owned by the memory UID from creation")
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	exec := shell.NewProcessExecutor(shell.DefaultAgentUID, 5*time.Second)
	res, err := exec.Run(context.Background(), shell.Request{Command: "cat " + dbPath})
	require.NoError(t, err)
	assert.NotZero(t, res.ExitCode)
	assert.Contains(t, res.Stderr, "Permission denied",
		"an agent-UID child gets EACCES reading memory.db")
}

// TestFactory_UnprivilegedSkipsMemoryDBChown pins the dev contract: an
// unprivileged platform attempts no chown and keeps owning memory.db.
func TestFactory_UnprivilegedSkipsMemoryDBChown(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("this case is only meaningful when the platform cannot chown")
	}
	root := t.TempDir()
	factory := &gateway.Factory{
		Root:     root,
		NewModel: replyModel,
	}
	rt, err := factory.New(context.Background(), "env-1", nil)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, rt.Close(context.Background()))
	}()

	info, err := os.Stat(filepath.Join(root, "env-1", gateway.MemoryDBFile))
	require.NoError(t, err)
	stat, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	assert.Equal(t, uint32(os.Geteuid()), stat.Uid,
		"the platform owns memory.db itself; no chown attempted")
}
