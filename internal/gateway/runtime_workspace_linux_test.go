//go:build linux

package gateway_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/files"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/shell"
	"github.com/garamsh/gagent/internal/tool"
)

// statUID returns the owner UID of the file at path.
func statUID(t *testing.T, path string) uint32 {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	stat, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	return stat.Uid
}

// writeViaFilesTool writes content to path inside the environment
// workspace through the runtime's files tool.
func writeViaFilesTool(t *testing.T, rt *gateway.Runtime, path, content string) {
	t.Helper()
	filesTool, err := rt.Registry.Lookup(files.ToolName)
	require.NoError(t, err)
	input, err := json.Marshal(map[string]string{
		"operation": "write",
		"path":      path,
		"content":   content,
	})
	require.NoError(t, err)
	_, err = tool.Invoke(context.Background(), filesTool, input)
	require.NoError(t, err)
}

// TestFactory_PrivilegedWorkspaceOwnedByAgentUID pins the ADR 0006
// agent-UID workspace domain: a privileged platform chowns the workspace
// to the agent UID at environment creation, the files tool writes files
// as that UID, and an agent-UID shell child can modify and delete them.
func TestFactory_PrivilegedWorkspaceOwnedByAgentUID(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("chown path requires root; run with `sudo -E go test ./internal/gateway/...` to exercise it")
	}
	root := t.TempDir()
	// t.TempDir is 0700; the agent-UID child must traverse down to the
	// workspace.
	require.NoError(t, os.Chmod(root, 0o755))
	factory := &gateway.Factory{
		Root:     root,
		NewModel: replyModel,
		NewShellExecutor: func() shell.Executor {
			return shell.NewProcessExecutor(shell.DefaultAgentUID, 5*time.Second)
		},
	}
	rt, err := factory.New(context.Background(), "env-1", nil)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, rt.Close(context.Background()))
	}()

	workspace := filepath.Join(root, "env-1", "workspace")
	assert.Equal(t, uint32(shell.DefaultAgentUID), statUID(t, workspace),
		"the workspace is owned by the agent UID from creation")

	writeViaFilesTool(t, rt, "note.txt", "hello")
	filePath := filepath.Join(workspace, "note.txt")
	assert.Equal(t, uint32(shell.DefaultAgentUID), statUID(t, filePath),
		"a files-tool write is owned by the agent UID")

	exec := shell.NewProcessExecutor(shell.DefaultAgentUID, 5*time.Second)
	res, err := exec.Run(context.Background(), shell.Request{
		Command: "echo more >> " + filePath + " && rm " + filePath,
	})
	require.NoError(t, err)
	assert.Zero(t, res.ExitCode,
		"an agent-UID child modifies and deletes a files-tool-created file: %s", res.Stderr)
}

// TestFactory_UnprivilegedSkipsWorkspaceChown pins the dev contract: an
// unprivileged platform attempts no chown and keeps owning the workspace
// and files-tool writes itself.
func TestFactory_UnprivilegedSkipsWorkspaceChown(t *testing.T) {
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

	workspace := filepath.Join(root, "env-1", "workspace")
	assert.Equal(t, uint32(os.Geteuid()), statUID(t, workspace),
		"the platform owns the workspace itself; no chown attempted")

	writeViaFilesTool(t, rt, "note.txt", "hello")
	assert.Equal(t, uint32(os.Geteuid()), statUID(t, filepath.Join(workspace, "note.txt")),
		"no setfsuid attempted; the platform owns the files-tool write itself")
}
