package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/config"
	"github.com/garamsh/gagent/internal/tool"
)

// pinnedToolTree installs a minimal manifest for the one required tool
// under a tmpdir and returns the directory and the manifest's hash; the
// caller points Config.ToolsDir at it. The tool needs no executable:
// every assertion here is about startup, which never spawns one.
//
// setToolEnv writes GAGENT_WORKSPACE_ADDR and GAGENT_OUTBOX_DIR through
// os.Setenv on the way, so the helper claims both through t.Setenv first
// and the test framework restores them.
func pinnedToolTree(t *testing.T) (dir, hash string) {
	t.Helper()
	dir = t.TempDir()
	toolDir := filepath.Join(dir, "message_send")
	require.NoError(t, os.MkdirAll(toolDir, 0o755))
	manifest := []byte(`{
		"name": "message_send",
		"description": "queue an outbound message",
		"command": "message_send",
		"input_schema": {
			"type": "object",
			"properties": {"content": {"type": "string"}},
			"required": ["content"]
		},
		"output_schema": {"type": "object"}
	}`)
	manifestPath := filepath.Join(toolDir, "manifest.json")
	require.NoError(t, os.WriteFile(manifestPath, manifest, 0o644))

	t.Setenv("GAGENT_WORKSPACE_ADDR", "")
	t.Setenv("GAGENT_OUTBOX_DIR", "")

	hash, err := tool.ManifestHashOfFile(manifestPath)
	require.NoError(t, err)
	return dir, hash
}

// TestBuildToolRegistry_DriftPinRefusesMutatedManifest is the drift
// guarantee measured on the wiring the agent actually runs (ADR 0009
// invariant 2). The pin is recorded, the manifest then changes on disk,
// and startup refuses it. A future edit that stops passing the
// operator's pin set into the registry fails here rather than turning
// the guarantee off in silence.
func TestBuildToolRegistry_DriftPinRefusesMutatedManifest(t *testing.T) {
	dir, hash := pinnedToolTree(t)
	cfg := config.Config{MemoryPath: filepath.Join(dir, "memory.db"), ToolsDir: dir}
	cfg.Tools.Pins = tool.ManifestPins{"message_send": hash}

	registry, err := buildToolRegistry(cfg, slog.New(slog.DiscardHandler))
	require.NoError(t, err, "the unmutated manifest matches its pin")
	require.NoError(t, registry.Close())

	mutated := []byte(`{
		"name": "message_send",
		"description": "ATTACKER-CONTROLLED DESCRIPTION",
		"command": "message_send",
		"input_schema": {
			"type": "object",
			"properties": {"content": {"type": "string"}},
			"required": ["content"]
		},
		"output_schema": {"type": "object"}
	}`)
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "message_send", "manifest.json"), mutated, 0o644))

	_, err = buildToolRegistry(cfg, slog.New(slog.DiscardHandler))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "descriptor drift")
}

// TestBuildToolRegistry_UnpinnedToolRefused holds the pin set to what
// its own contract says it is: the operator's accepted tool set. A
// directory that appears in the tree without a pin is unreviewed, so it
// does not load.
func TestBuildToolRegistry_UnpinnedToolRefused(t *testing.T) {
	dir, hash := pinnedToolTree(t)
	extraDir := filepath.Join(dir, "exfiltrate")
	require.NoError(t, os.MkdirAll(extraDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(extraDir, "manifest.json"), []byte(`{
		"name": "exfiltrate",
		"description": "unreviewed",
		"command": "exfiltrate",
		"input_schema": {"type": "object"},
		"output_schema": {"type": "object"}
	}`), 0o644))

	cfg := config.Config{MemoryPath: filepath.Join(dir, "memory.db"), ToolsDir: dir}
	cfg.Tools.Pins = tool.ManifestPins{"message_send": hash}

	_, err := buildToolRegistry(cfg, slog.New(slog.DiscardHandler))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unpinned tool "exfiltrate"`)
}

// TestBuildToolRegistry_WithoutPinsLoads states what an agent without a
// tools.pins section gets: the tools load, and the drift guarantee is
// off. The pin is the operator's record of what they accepted, so the
// platform has none to check against until the operator writes one.
func TestBuildToolRegistry_WithoutPinsLoads(t *testing.T) {
	dir, _ := pinnedToolTree(t)
	cfg := config.Config{MemoryPath: filepath.Join(dir, "memory.db"), ToolsDir: dir}

	registry, err := buildToolRegistry(cfg, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	t.Cleanup(func() { _ = registry.Close() })
	assert.Len(t, registry.List(), 1)
}
