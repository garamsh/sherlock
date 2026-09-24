package tool_test

import (
	"os"
	"path/filepath"
	"regexp"
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
	t.Setenv(tool.AgentIDEnv, "")

	require.NoError(t, tool.SetSpawnEnv("127.0.0.1:9091", "/srv/agent/outbox", "agent-7"))

	assert.Equal(t, "127.0.0.1:9091", os.Getenv(tool.WorkspaceAddrEnv))
	assert.Equal(t, "/srv/agent/outbox", os.Getenv(tool.OutboxDirEnv))
	assert.Equal(t, "agent-7", os.Getenv(tool.AgentIDEnv))
}

// getenvCall matches a shipped tool reading a variable by name.
var getenvCall = regexp.MustCompile(`os\.Getenv\("([A-Z0-9_]+)"\)`)

// TestSetSpawnEnv_CoversWhatTheShippedToolsRead holds the platform's
// constants against what the shipped tools actually read. A tool that
// starts reading another variable is either one the operator supplies,
// like the github token, or one this function must set — and this is
// where that decision surfaces instead of becoming a tool that starts
// up without its value.
//
// The source is the tools' own os.Getenv calls. It used to be their
// manifests' env arrays, which #550 deleted: that field was inert, and
// it was also an incomplete record of what the tools read — tools/github
// reads GITHUB_API_BASE and never declared it, so the variable below was
// invisible to this check for as long as the check trusted the manifest.
// Reading the calls cannot drift from the code the way a hand-maintained
// declaration did.
func TestSetSpawnEnv_CoversWhatTheShippedToolsRead(t *testing.T) {
	// Set by the operator, never by the platform: a materialized secret
	// is a stolen secret (ADR 0003, ADR 0013), and the platform does not
	// dispatch secrets (ADR 0009). GITHUB_API_BASE is not a secret but is
	// the operator's too — it retargets the API for an enterprise host,
	// and the default is the public one.
	operatorSupplied := map[string]bool{
		"GH_TOKEN":                true,
		"GITHUB_API_BASE":         true,
		"GITHUB_REPOSITORY":       true,
		"GITHUB_BRANCH":           true,
		"GITHUB_VALIDATE_COMMAND": true,
	}
	platformSet := map[string]bool{
		tool.WorkspaceAddrEnv: true,
		tool.OutboxDirEnv:     true,
		tool.AgentIDEnv:       true,
	}

	read := map[string]string{}
	toolsDir := filepath.Join("..", "..", "tools")
	entries, err := os.ReadDir(toolsDir)
	require.NoError(t, err)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sources, err := filepath.Glob(filepath.Join(toolsDir, e.Name(), "*.go"))
		require.NoError(t, err)
		for _, src := range sources {
			body, err := os.ReadFile(src)
			require.NoError(t, err)
			for _, m := range getenvCall.FindAllStringSubmatch(string(body), -1) {
				read[m[1]] = e.Name()
			}
		}
	}
	require.NotEmpty(t, read, "the shipped tools read their inputs from the environment")

	for name, toolName := range read {
		if operatorSupplied[name] {
			continue
		}
		assert.True(t, platformSet[name],
			"tools/%s reads %s, which nothing sets: add it to SetSpawnEnv or to the operator-supplied set",
			toolName, name)
	}
	for name := range platformSet {
		assert.Contains(t, read, name,
			"the platform sets %s but no shipped tool reads it", name)
	}
}
