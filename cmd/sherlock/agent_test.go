package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/config"
	"github.com/garamsh/sherlock/internal/tool"
)

// pinnedToolTree installs a minimal manifest for the one required tool
// under a tmpdir and returns the directory and the manifest's hash; the
// caller points Config.ToolsDir at it. The tool needs no executable:
// every assertion here is about startup, which never spawns one.
//
// tool.SetSpawnEnv writes SHERLOCK_WORKSPACE_ADDR, SHERLOCK_OUTBOX_DIR and
// SHERLOCK_AGENT_ID through os.Setenv on the way, so the helper claims the
// first two through t.Setenv first and the framework restores them.
// SHERLOCK_AGENT_ID is not claimed and outlives the test.
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

	t.Setenv("SHERLOCK_WORKSPACE_ADDR", "")
	t.Setenv("SHERLOCK_OUTBOX_DIR", "")

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

	_, err := buildToolRegistry(cfg, "test-agent", slog.New(slog.DiscardHandler))
	require.NoError(t, err, "the unmutated manifest matches its pin")

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

	_, err = buildToolRegistry(cfg, "test-agent", slog.New(slog.DiscardHandler))
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

	_, err := buildToolRegistry(cfg, "test-agent", slog.New(slog.DiscardHandler))
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

	registry, err := buildToolRegistry(cfg, "test-agent", slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	assert.Len(t, registry.List(), 1)
}

// TestBuildToolRegistry_RequiresTheMouthAndNothingElse pins the extent
// of the required-tool guarantee (#564, ADR 0009 invariant 5). Two
// halves, and the second is the one the documents used to overstate.
//
// A tree without message_send fails startup closed: the agent cannot
// speak, so there is no useful agent to start. A tree holding only
// message_send starts cleanly — the workspace-coupling tools are
// capabilities the operator installs or does not (ADR 0017), so their
// absence is a choice rather than a failure, and there is no operator
// key that would add them to the required set.
func TestBuildToolRegistry_RequiresTheMouthAndNothingElse(t *testing.T) {
	t.Run("the mouth alone is enough to start", func(t *testing.T) {
		dir, _ := pinnedToolTree(t)
		cfg := config.Config{MemoryPath: filepath.Join(dir, "memory.db"), ToolsDir: dir}

		registry, err := buildToolRegistry(cfg, "test-agent", slog.New(slog.DiscardHandler))
		require.NoError(t, err, "files and shell_exec are absent and the agent still starts")

		names := make([]string, 0, 1)
		for _, d := range registry.List() {
			names = append(names, d.Name)
		}
		assert.Equal(t, []string{"message_send"}, names,
			"the tree holds one tool and the agent runs on it")
	})

	t.Run("without the mouth startup fails closed", func(t *testing.T) {
		dir, _ := pinnedToolTree(t)
		require.NoError(t, os.RemoveAll(filepath.Join(dir, "message_send")))
		cfg := config.Config{MemoryPath: filepath.Join(dir, "memory.db"), ToolsDir: dir}

		_, err := buildToolRegistry(cfg, "test-agent", slog.New(slog.DiscardHandler))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "required tools missing")
		assert.Contains(t, err.Error(), "message_send")
	})
}

// TestServeAgent_FailedRegistryLeavesNothingListening pins the ordering
// a downstream readiness probe rests on (#477): no listener exists
// before the tool registry has loaded, so a failed load leaves the port
// refusing connections rather than answering for a process that cannot
// accept work. An edit that binds first and fails afterwards satisfies
// every other startup rule and fails here — during rollout and
// crashloop, which is when a probe is the only reader.
func TestServeAgent_FailedRegistryLeavesNothingListening(t *testing.T) {
	dir, _ := pinnedToolTree(t)
	// A manifest that does not parse fails the registry load (ADR 0009).
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "message_send", "manifest.json"), []byte("{"), 0o644))

	// Reserve an address the way the listener would take it, then let it
	// go: anything answering on it after the failed startup is the bug.
	listen, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listen.Addr().String()
	require.NoError(t, listen.Close())

	cfg := config.Config{
		Addr:       addr,
		MemoryPath: filepath.Join(dir, "memory.db"),
		ToolsDir:   dir,
	}

	// Probe while the startup is in flight, not after it: a bind-first
	// ordering opens the window between the bind and the failed load,
	// and a dial after serveAgent has returned misses it.
	done := make(chan error, 1)
	go func() { done <- serveAgent(context.Background(), cfg, "test-agent", "") }()
	var answered bool
	for {
		select {
		case err := <-done:
			require.Error(t, err, "the registry load fails")
			assert.False(t, answered,
				"the port answered during a startup that never loaded its registry")
			return
		default:
			conn, dialErr := net.DialTimeout("tcp", addr, 20*time.Millisecond)
			if dialErr == nil {
				answered = true
				_ = conn.Close()
			}
		}
	}
}
