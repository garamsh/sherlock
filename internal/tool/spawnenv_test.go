package tool_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/tool"
)

// TestSetSpawnEnv_PublishesWhatTheToolsRead holds the spawn contract
// from the reading end: the runner hands each tool the platform's
// environment, so a value set here is a value the tool finds.
func TestSetSpawnEnv_PublishesWhatTheToolsRead(t *testing.T) {
	t.Setenv(tool.WorkspaceAddrEnv, "")
	t.Setenv(tool.OutboxDirEnv, "")

	require.NoError(t, tool.SetSpawnEnv("127.0.0.1:9091", "/srv/agent/outbox"))

	assert.Equal(t, "127.0.0.1:9091", os.Getenv(tool.WorkspaceAddrEnv))
	assert.Equal(t, "/srv/agent/outbox", os.Getenv(tool.OutboxDirEnv))
}

// TestSetSpawnEnv_CoversTheShippedManifests reads what the shipped
// tools declare they need and holds the platform's two constants
// against it. A manifest that starts declaring a third variable is
// either one the operator supplies, like the github token, or one this
// function must set — and this is where that decision surfaces instead
// of becoming a tool that starts up without its value.
func TestSetSpawnEnv_CoversTheShippedManifests(t *testing.T) {
	// Set by the operator, never by the platform: a materialized secret
	// is a stolen secret (ADR 0003, ADR 0013).
	operatorSupplied := map[string]bool{"GH_TOKEN": true}
	platformSet := map[string]bool{
		tool.WorkspaceAddrEnv: true,
		tool.OutboxDirEnv:     true,
	}

	declared := map[string]string{}
	entries, err := os.ReadDir(filepath.Join("..", "..", "tools"))
	require.NoError(t, err)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("..", "..", "tools", e.Name(), "manifest.json"))
		require.NoError(t, err)
		var m struct {
			Env []string `json:"env"`
		}
		require.NoError(t, json.Unmarshal(raw, &m))
		for _, name := range m.Env {
			declared[name] = e.Name()
		}
	}
	require.NotEmpty(t, declared, "the shipped tools declare the env they read")

	for name, toolName := range declared {
		if operatorSupplied[name] {
			continue
		}
		assert.True(t, platformSet[name],
			"tools/%s declares %s, which nothing sets: add it to SetSpawnEnv or to the operator-supplied set",
			toolName, name)
	}
	for name := range platformSet {
		assert.Contains(t, declared, name,
			"the platform sets %s but no shipped manifest declares it", name)
	}
}
