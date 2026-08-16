package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/config"
)

// TestServeWorkspace_NonLoopbackAddrFailsStartup pins the fail-closed
// posture: the gRPC listener is unauthenticated in this phase, so a
// non-loopback bind is a remote shell into the workspace and fails
// startup (ADR 0012).
func TestServeWorkspace_NonLoopbackAddrFailsStartup(t *testing.T) {
	cfg := config.WorkspaceConfig{
		Addr:      "0.0.0.0:8081",
		Workspace: filepath.Join(t.TempDir(), "workspace"),
	}

	err := serveWorkspace(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loopback",
		"the startup error must state the loopback-only requirement")
	assert.Contains(t, err.Error(), "unauthenticated",
		"the startup error must state the reason")
}
