package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/config"
	"github.com/garamsh/sherlock/internal/gateway"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
	"github.com/garamsh/sherlock/internal/tool"
)

// testIdentity is the identity the startup tests serve under.
var testIdentity = identity{agentID: "test-agent"}

// pinnedToolTree installs a minimal manifest for the one required tool
// under a tmpdir and returns the directory and the manifest's hash; the
// caller points Config.ToolsDir at it. The tool needs no executable:
// every assertion here is about startup, which never spawns one.
//
// tool.SetSpawnEnv writes SHERLOCK_WORKSPACE_ADDR, SHERLOCK_OUTBOX_DIR,
// SHERLOCK_AGENT_ID, SHERLOCK_ASSIGNMENT_EPOCH and SHERLOCK_GENERATION
// through os.Setenv on the way, so the helper claims all but the agent ID
// through t.Setenv first and the framework restores them.
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
	t.Setenv("SHERLOCK_ASSIGNMENT_EPOCH", "")
	t.Setenv("SHERLOCK_GENERATION", "")

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

	_, err := buildToolRegistry(cfg, testIdentity, slog.New(slog.DiscardHandler))
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

	_, err = buildToolRegistry(cfg, testIdentity, slog.New(slog.DiscardHandler))
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

	_, err := buildToolRegistry(cfg, testIdentity, slog.New(slog.DiscardHandler))
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

	registry, err := buildToolRegistry(cfg, testIdentity, slog.New(slog.DiscardHandler))
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

		registry, err := buildToolRegistry(cfg, testIdentity, slog.New(slog.DiscardHandler))
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

		_, err := buildToolRegistry(cfg, testIdentity, slog.New(slog.DiscardHandler))
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
	go func() { done <- serveAgent(context.Background(), cfg, testIdentity, "") }()
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

// TestAgentCommand_RefusesWithoutAgentID holds that the agent serves
// the identity it was given and never a fallback (ADR 0016): an absent
// and an empty --agent-id both refuse startup with errAgentIDRequired.
//
// The control runs the same command with a GRN-shaped ID. It gets past
// the identity check and fails later, on the missing tools directory,
// having published the ID verbatim to the tools' environment — so the
// refusal above is the identity check's and not a startup that fails for
// every input.
func TestAgentCommand_RefusesWithoutAgentID(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(configPath, nil, 0o600))
	t.Setenv("SHERLOCK_TOOLS_DIR", filepath.Join(t.TempDir(), "absent"))
	t.Setenv(tool.WorkspaceAddrEnv, "")
	t.Setenv(tool.OutboxDirEnv, "")
	t.Setenv(tool.AgentIDEnv, "")

	run := func(args ...string) error {
		root := newRootCommand()
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		root.SetArgs(append([]string{"agent", "--config", configPath,
			"--memory-path", filepath.Join(t.TempDir(), "memory.db"),
			"--message", "hello"}, args...))
		return root.Execute()
	}

	t.Run("absent", func(t *testing.T) {
		assert.ErrorIs(t, run(), errAgentIDRequired)
	})
	t.Run("empty", func(t *testing.T) {
		assert.ErrorIs(t, run("--agent-id="), errAgentIDRequired)
	})
	t.Run("control: a GRN passes the check and is published verbatim", func(t *testing.T) {
		const grn = "grn:garam:default:agent:144460a71649d1a1"
		err := run("--agent-id", grn)
		require.Error(t, err)
		assert.NotErrorIs(t, err, errAgentIDRequired)
		assert.Contains(t, err.Error(), "tools directory")
		assert.Equal(t, grn, os.Getenv(tool.AgentIDEnv))
	})
}

// TestResolveIdentity_CarriesTheAssignmentEpoch holds the launcher's
// assignment epoch as given: opaque and verbatim when set, empty
// (unknown) when absent, and never a reason to refuse startup. The
// generation is not a flag, so resolution leaves it unminted.
func TestResolveIdentity_CarriesTheAssignmentEpoch(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want identity
	}{
		{"set", []string{"--agent-id", "a1", "--assignment-epoch", "epoch:7"},
			identity{agentID: "a1", assignmentEpoch: "epoch:7"}},
		{"absent", []string{"--agent-id", "a1"}, identity{agentID: "a1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := pflag.NewFlagSet("agent", pflag.ContinueOnError)
			addAgentFlags(fs)
			require.NoError(t, fs.Parse(tt.args))

			got, err := resolveIdentity(fs)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestAgentCommand_GenerationIsNotAFlag holds contract v1.2: the
// generation is minted per start, so a launcher cannot pass one.
func TestAgentCommand_GenerationIsNotAFlag(t *testing.T) {
	fs := pflag.NewFlagSet("agent", pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addAgentFlags(fs)
	err := fs.Parse([]string{"--agent-id", "a1", "--generation", "3"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown flag: --generation")
}

// mockFactory builds the serving factory over the mock model and a
// minimal tool tree, on a memory path under a tmpdir.
func mockFactory(t *testing.T) (*gateway.Factory, string) {
	t.Helper()
	dir, _ := pinnedToolTree(t)
	cfg := config.Config{
		MemoryPath: filepath.Join(dir, "memory.db"),
		ToolsDir:   dir,
		Model:      config.ModelConfig{Provider: "mock"},
	}
	factory, err := newAgentFactory(cfg, testIdentity, "", slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	return factory, cfg.MemoryPath
}

// TestStartQueue_MintsTheGenerationOnlyUnderTheStoreLock holds the
// generation's ordering (contract v1.2): it is minted after the memory
// store's writer lock is held, so a start refused by a store another
// process holds mints none. The control is the same start once the
// holder closes: it mints exactly one.
func TestStartQueue_MintsTheGenerationOnlyUnderTheStoreLock(t *testing.T) {
	factory, memoryPath := mockFactory(t)
	minted := 0
	mint := func() string { minted++; return mintGeneration() }
	logger := slog.New(slog.DiscardHandler)

	held, err := sqlite.Open(memoryPath)
	require.NoError(t, err)
	_, _, err = startQueue(factory, testIdentity, mint, logger)
	assert.ErrorIs(t, err, sqlite.ErrStoreLocked)
	assert.Zero(t, minted, "a start the store lock refused minted a generation")

	require.NoError(t, held.Close())
	queue, id, err := startQueue(factory, testIdentity, mint, logger)
	require.NoError(t, err, "the control: the released store starts")
	t.Cleanup(func() { queue.Close() })
	assert.Equal(t, 1, minted)
	assert.NotEmpty(t, id.generation)
	assert.Equal(t, id.generation, os.Getenv(tool.GenerationEnv),
		"the tools are handed the generation this start minted")
}

// generationShape is a runtime start nonce: 128 bits as lowercase hex.
var generationShape = regexp.MustCompile(`^[0-9a-f]{32}$`)

// TestStartQueue_EachStartMintsAFreshGeneration holds that the
// generation is per process start and never persisted (ADR 0016): two
// starts on the same memory store, one after the other, carry different
// 32-character lowercase hex generations.
func TestStartQueue_EachStartMintsAFreshGeneration(t *testing.T) {
	factory, _ := mockFactory(t)
	logger := slog.New(slog.DiscardHandler)

	generations := make([]string, 0, 2)
	for range 2 {
		queue, id, err := startQueue(factory, testIdentity, mintGeneration, logger)
		require.NoError(t, err)
		require.NoError(t, queue.Close(), "closing releases the store for the next start")
		assert.Regexp(t, generationShape, id.generation)
		generations = append(generations, id.generation)
	}
	assert.NotEqual(t, generations[0], generations[1])
}

// TestInstanceOf_StatusReportsWhatThisStartLoaded holds C3's sources:
// the status route reports the agent ID and assignment epoch the flags
// gave, the generation this start minted, the revision the config file
// carries, and the process start time.
func TestInstanceOf_StatusReportsWhatThisStartLoaded(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "configs", "agent.yaml"), []byte("revision: rev-9\n"), 0o600))
	fs := pflag.NewFlagSet("agent", pflag.ContinueOnError)
	config.AddSharedFlags(fs)
	addAgentFlags(fs)
	require.NoError(t, fs.Parse([]string{"--agent-id", "a1", "--assignment-epoch", "e3"}))
	cfg, err := config.Resolve(fs)
	require.NoError(t, err)
	id, err := resolveIdentity(fs)
	require.NoError(t, err)

	factory, _ := mockFactory(t)
	logger := slog.New(slog.DiscardHandler)
	queue, id, err := startQueue(factory, id, func() string { return "gen-1" }, logger)
	require.NoError(t, err)
	t.Cleanup(func() { queue.Close() })
	srv := httptest.NewServer(gateway.NewHandler(queue, instanceOf(cfg, id), logger))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/agents/a1/status")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var got struct {
		AgentID         string    `json:"agent_id"`
		AssignmentEpoch string    `json:"assignment_epoch"`
		Generation      string    `json:"generation"`
		ConfigRevision  string    `json:"config_revision"`
		StartedAt       time.Time `json:"started_at"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, "a1", got.AgentID)
	assert.Equal(t, "e3", got.AssignmentEpoch)
	assert.Equal(t, "gen-1", got.Generation)
	assert.Equal(t, "rev-9", got.ConfigRevision)
	assert.True(t, processStart.Equal(got.StartedAt), "started_at is the process start time")
}
