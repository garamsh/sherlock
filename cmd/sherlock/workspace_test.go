package main_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sherlock "github.com/garamsh/sherlock/cmd/sherlock"
)

// TestWorkspaceCommand_NonLoopbackAddrFailsStartup pins the fail-closed
// posture: the gRPC listener is unauthenticated in this phase, so a
// non-loopback bind is a remote shell into the workspace and fails
// startup (ADR 0012).
func TestWorkspaceCommand_NonLoopbackAddrFailsStartup(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "workspace.yaml")
	require.NoError(t, os.WriteFile(configPath, nil, 0o600))
	root := sherlock.NewRootCommand()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"workspace", "--config", configPath,
		"--addr", "0.0.0.0:8081", "--workspace", filepath.Join(t.TempDir(), "workspace")})

	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loopback",
		"the startup error must state the loopback-only requirement")
	assert.Contains(t, err.Error(), "unauthenticated",
		"the startup error must state the reason")
}
