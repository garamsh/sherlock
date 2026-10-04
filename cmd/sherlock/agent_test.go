package main_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sherlock "github.com/garamsh/sherlock/cmd/sherlock"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
	"github.com/garamsh/sherlock/internal/tool"
)

// pinnedToolTree installs a minimal manifest for the one required tool
// under a tmpdir and returns the directory and the manifest's hash; the
// caller names the directory in the config file as tools-dir. The tool
// needs no executable: a turn that calls it records a failed call, and
// every assertion here is about startup or about which calls failed.
//
// The agent publishes SHERLOCK_WORKSPACE_ADDR, SHERLOCK_OUTBOX_DIR,
// SHERLOCK_AGENT_ID, SHERLOCK_ASSIGNMENT_EPOCH and SHERLOCK_GENERATION
// through os.Setenv as it starts, so the helper claims them through
// t.Setenv first and the framework restores them.
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

	t.Setenv(tool.WorkspaceAddrEnv, "")
	t.Setenv(tool.OutboxDirEnv, "")
	t.Setenv(tool.AgentIDEnv, "")
	t.Setenv(tool.AssignmentEpochEnv, "")
	t.Setenv(tool.GenerationEnv, "")

	hash, err := tool.ManifestHashOfFile(manifestPath)
	require.NoError(t, err)
	return dir, hash
}

// writeConfig writes body as an owner-only agent config file under a
// tmpdir and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// mockConfig is a config running the mock model over the tools in dir,
// with pins as its tools.pins section when pins is not empty.
func mockConfig(t *testing.T, dir string, pins map[string]string) string {
	t.Helper()
	body := fmt.Sprintf("tools-dir: %q\nmodel:\n  provider: mock\n", dir)
	if len(pins) > 0 {
		body += "tools:\n  pins:\n"
		for name, hash := range pins {
			body += fmt.Sprintf("    %s: %q\n", name, hash)
		}
	}
	return writeConfig(t, body)
}

// runAgent runs `sherlock agent` with args through the binary's command
// tree and returns its error.
func runAgent(ctx context.Context, args ...string) error {
	root := sherlock.NewRootCommand()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs(append([]string{"agent"}, args...))
	return root.ExecuteContext(ctx)
}

// runTurn runs one `sherlock agent --message` turn as test-agent under
// configPath on memoryPath, with args added.
func runTurn(configPath, memoryPath string, args ...string) error {
	return runAgent(context.Background(), append([]string{"--config", configPath,
		"--memory-path", memoryPath, "--agent-id", "test-agent", "--message", "hello"}, args...)...)
}

// mockTurnRan is the error a one-shot turn of the mock model returns
// over a tree holding only message_send's manifest: the scripted
// scenario calls shell_exec and file_read, which are not installed, and
// message_send, which has no executable. A turn that returns it got past
// every startup check and ran the scripted model.
const mockTurnRan = "one-shot turn: 3 tool call(s) failed: shell_exec, file_read, message_send"

// freeAddr reserves a loopback address the way the listener would take
// it, then lets it go.
func freeAddr(t *testing.T) string {
	t.Helper()
	listen, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listen.Addr().String()
	require.NoError(t, listen.Close())
	return addr
}

// agentStatus is the status route's body.
type agentStatus struct {
	AgentID         string    `json:"agent_id"`
	AssignmentEpoch string    `json:"assignment_epoch"`
	Generation      string    `json:"generation"`
	ConfigRevision  string    `json:"config_revision"`
	StartedAt       time.Time `json:"started_at"`
}

// serve starts `sherlock agent` serving on a free address as agentID
// under configPath on memoryPath, with args added, waits for its status
// route to answer, and returns what it reported and a stop function that
// ends the serve and returns its error.
func serve(t *testing.T, configPath, memoryPath, agentID string, args ...string) (agentStatus, func() error) {
	t.Helper()
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runAgent(ctx, append([]string{"--config", configPath, "--memory-path", memoryPath,
			"--addr", addr, "--agent-id", agentID}, args...)...)
	}()
	stop := func() error { cancel(); return <-done }

	var status agentStatus
	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + addr + "/agents/" + agentID + "/status")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(&status) == nil
	}, 5*time.Second, 20*time.Millisecond, "the agent serves its status route")
	return status, stop
}

// TestAgentCommand_DriftPinRefusesMutatedManifest is the drift guarantee
// measured on the wiring the agent actually runs (ADR 0009 invariant 2).
// The pin is recorded, the manifest then changes on disk, and startup
// refuses it. A future edit that stops passing the operator's pin set
// into the registry fails here rather than turning the guarantee off in
// silence.
func TestAgentCommand_DriftPinRefusesMutatedManifest(t *testing.T) {
	dir, hash := pinnedToolTree(t)
	configPath := mockConfig(t, dir, map[string]string{"message_send": hash})
	memoryPath := filepath.Join(t.TempDir(), "memory.db")

	assert.EqualError(t, runTurn(configPath, memoryPath), mockTurnRan,
		"the unmutated manifest matches its pin and the turn runs")

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

	err := runTurn(configPath, memoryPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "descriptor drift")
}

// TestAgentCommand_UnpinnedToolRefused holds the pin set to what its own
// contract says it is: the operator's accepted tool set. A directory
// that appears in the tree without a pin is unreviewed, so it does not
// load.
func TestAgentCommand_UnpinnedToolRefused(t *testing.T) {
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
	configPath := mockConfig(t, dir, map[string]string{"message_send": hash})

	err := runTurn(configPath, filepath.Join(t.TempDir(), "memory.db"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unpinned tool "exfiltrate"`)
}

// TestAgentCommand_WithoutPinsLoads states what an agent without a
// tools.pins section gets: the tools load, and the drift guarantee is
// off. The pin is the operator's record of what they accepted, so the
// platform has none to check against until the operator writes one.
func TestAgentCommand_WithoutPinsLoads(t *testing.T) {
	dir, _ := pinnedToolTree(t)
	configPath := mockConfig(t, dir, nil)

	assert.EqualError(t, runTurn(configPath, filepath.Join(t.TempDir(), "memory.db")), mockTurnRan)
}

// TestAgentCommand_RequiresTheMouthAndNothingElse pins the extent of the
// required-tool guarantee (#564, ADR 0009 invariant 5). Two halves, and
// the second is the one the documents used to overstate.
//
// A tree without message_send fails startup closed: the agent cannot
// speak, so there is no useful agent to start. A tree holding only
// message_send starts cleanly — the workspace-coupling tools are
// capabilities the operator installs or does not (ADR 0017), so their
// absence is a choice rather than a failure, and there is no operator
// key that would add them to the required set.
func TestAgentCommand_RequiresTheMouthAndNothingElse(t *testing.T) {
	t.Run("the mouth alone is enough to start", func(t *testing.T) {
		dir, _ := pinnedToolTree(t)
		configPath := mockConfig(t, dir, nil)

		assert.EqualError(t, runTurn(configPath, filepath.Join(t.TempDir(), "memory.db")), mockTurnRan,
			"files and shell_exec are absent and the agent still starts")
	})

	t.Run("without the mouth startup fails closed", func(t *testing.T) {
		dir, _ := pinnedToolTree(t)
		require.NoError(t, os.RemoveAll(filepath.Join(dir, "message_send")))
		configPath := mockConfig(t, dir, nil)

		err := runTurn(configPath, filepath.Join(t.TempDir(), "memory.db"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "required tools missing")
		assert.Contains(t, err.Error(), "message_send")
	})
}

// TestAgentCommand_FailedRegistryLeavesNothingListening pins the
// ordering a downstream readiness probe rests on (#477): no listener
// exists before the tool registry has loaded, so a failed load leaves
// the port refusing connections rather than answering for a process that
// cannot accept work. An edit that binds first and fails afterwards
// satisfies every other startup rule and fails here — during rollout and
// crashloop, which is when a probe is the only reader.
func TestAgentCommand_FailedRegistryLeavesNothingListening(t *testing.T) {
	dir, _ := pinnedToolTree(t)
	// A manifest that does not parse fails the registry load (ADR 0009).
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "message_send", "manifest.json"), []byte("{"), 0o644))
	configPath := mockConfig(t, dir, nil)
	// Anything answering on the address after the failed startup is the bug.
	addr := freeAddr(t)

	// Probe while the startup is in flight, not after it: a bind-first
	// ordering opens the window between the bind and the failed load,
	// and a dial after the command has returned misses it.
	done := make(chan error, 1)
	go func() {
		done <- runAgent(context.Background(), "--config", configPath,
			"--memory-path", filepath.Join(t.TempDir(), "memory.db"),
			"--addr", addr, "--agent-id", "test-agent")
	}()
	var answered bool
	for {
		select {
		case err := <-done:
			require.Error(t, err, "the registry load fails")
			assert.Contains(t, err.Error(), "tool registry")
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

// TestAgentCommand_RequireStoreRefusesBeforeListening holds the
// refusal's place in a served start (ADR 0025): with --require-store and
// no store, the start fails naming the path and the flag, nothing
// answers on the port while it does, and no store is created. The
// control is the same start on a store an earlier start created: it gets
// past the check and listens.
func TestAgentCommand_RequireStoreRefusesBeforeListening(t *testing.T) {
	dir, _ := pinnedToolTree(t)
	configPath := mockConfig(t, dir, nil)
	memoryPath := filepath.Join(t.TempDir(), "memory.db")
	addr := freeAddr(t)
	args := []string{"--config", configPath, "--memory-path", memoryPath,
		"--addr", addr, "--agent-id", "test-agent", "--require-store"}

	done := make(chan error, 1)
	go func() { done <- runAgent(context.Background(), args...) }()
	var answered bool
	deadline := time.After(5 * time.Second)
	for refused := false; !refused; {
		select {
		case <-deadline:
			t.Fatal("the start did not refuse the absent store")
		case err := <-done:
			require.ErrorIs(t, err, sqlite.ErrStoreUninitialized)
			assert.ErrorContains(t, err, memoryPath)
			assert.ErrorContains(t, err, "--require-store")
			refused = true
		default:
			if conn, dialErr := net.DialTimeout("tcp", addr, 20*time.Millisecond); dialErr == nil {
				answered = true
				_ = conn.Close()
			}
		}
	}
	assert.False(t, answered, "the port answered during a start the store check refused")
	assert.NoFileExists(t, memoryPath)

	store, err := sqlite.OpenConversation(memoryPath, nil)
	require.NoError(t, err)
	require.NoError(t, store.Close())
	ctx, cancel := context.WithCancel(context.Background())
	go func() { done <- runAgent(ctx, args...) }()
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", addr, 20*time.Millisecond)
		if err == nil {
			_ = conn.Close()
		}
		return err == nil
	}, 5*time.Second, 20*time.Millisecond, "control: the initialized store starts and listens")
	cancel()
	assert.NoError(t, <-done)
}

// TestAgentCommand_RequireStoreIsTheFlagThatRefuses holds the flag's
// wiring from the command line to the store (ADR 0025): `sherlock agent
// --require-store` on an absent store fails naming the flag and creates
// no store. The control is the same command without the flag, which
// creates the store as every first start does.
func TestAgentCommand_RequireStoreIsTheFlagThatRefuses(t *testing.T) {
	toolsDir, _ := pinnedToolTree(t)
	configPath := writeConfig(t, "")
	t.Setenv("SHERLOCK_TOOLS_DIR", toolsDir)
	memoryPath := filepath.Join(t.TempDir(), "memory.db")

	err := runTurn(configPath, memoryPath, "--require-store")
	require.ErrorIs(t, err, sqlite.ErrStoreUninitialized)
	assert.ErrorContains(t, err, "--require-store")
	assert.NoFileExists(t, memoryPath)

	err = runTurn(configPath, memoryPath)
	assert.NotErrorIs(t, err, sqlite.ErrStoreUninitialized)
	assert.FileExists(t, memoryPath, "control: without the flag the first start creates the store")
}

// agentIDRequired is the refusal of a start with no identity (ADR 0016).
const agentIDRequired = "--agent-id is required"

// TestAgentCommand_RefusesWithoutAgentID holds that the agent serves
// the identity it was given and never a fallback (ADR 0016): an absent
// and an empty --agent-id both refuse startup.
//
// The control runs the same command with a GRN-shaped ID. It gets past
// the identity check and fails later, on the missing tools directory,
// having published the ID verbatim to the tools' environment — so the
// refusal above is the identity check's and not a startup that fails for
// every input.
func TestAgentCommand_RefusesWithoutAgentID(t *testing.T) {
	configPath := writeConfig(t, "")
	t.Setenv("SHERLOCK_TOOLS_DIR", filepath.Join(t.TempDir(), "absent"))
	t.Setenv(tool.WorkspaceAddrEnv, "")
	t.Setenv(tool.OutboxDirEnv, "")
	t.Setenv(tool.AgentIDEnv, "")

	run := func(args ...string) error {
		return runAgent(context.Background(), append([]string{"--config", configPath,
			"--memory-path", filepath.Join(t.TempDir(), "memory.db"),
			"--message", "hello"}, args...)...)
	}

	t.Run("absent", func(t *testing.T) {
		assert.ErrorContains(t, run(), agentIDRequired)
	})
	t.Run("empty", func(t *testing.T) {
		assert.ErrorContains(t, run("--agent-id="), agentIDRequired)
	})
	t.Run("control: a GRN passes the check and is published verbatim", func(t *testing.T) {
		const grn = "grn:garam:default:agent:144460a71649d1a1"
		err := run("--agent-id", grn)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), agentIDRequired)
		assert.Contains(t, err.Error(), "tools directory")
		assert.Equal(t, grn, os.Getenv(tool.AgentIDEnv))
	})
}

// TestAgentCommand_MissingInstructionsFileFailsStartup holds that a named
// --instructions-file that cannot be read stops the agent before any
// tool or model work, as --ego-file does (ADR 0024).
//
// The control names a file that exists. It gets past the instructions
// read and fails later, on the missing tools directory — so the refusal
// is the read's and not a startup that fails for every input.
func TestAgentCommand_MissingInstructionsFileFailsStartup(t *testing.T) {
	configPath := writeConfig(t, "")
	t.Setenv("SHERLOCK_TOOLS_DIR", filepath.Join(t.TempDir(), "absent"))
	t.Setenv(tool.WorkspaceAddrEnv, "")
	t.Setenv(tool.OutboxDirEnv, "")
	t.Setenv(tool.AgentIDEnv, "")

	run := func(instructionsFile string) error {
		return runTurn(configPath, filepath.Join(t.TempDir(), "memory.db"),
			"--instructions-file", instructionsFile)
	}

	t.Run("missing file", func(t *testing.T) {
		err := run(filepath.Join(t.TempDir(), "absent.md"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "instructions file")
		assert.NotContains(t, err.Error(), "tools directory")
	})
	t.Run("control: a readable file passes the read", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "INSTRUCTIONS.md")
		require.NoError(t, os.WriteFile(path, []byte("Answer every envelope.\n"), 0o600))
		err := run(path)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "instructions file")
		assert.Contains(t, err.Error(), "tools directory")
	})
}

// TestAgentCommand_StatusCarriesTheAssignmentEpoch holds the launcher's
// assignment epoch as given: opaque and verbatim when set, empty
// (unknown) when absent, and never a reason to refuse startup.
func TestAgentCommand_StatusCarriesTheAssignmentEpoch(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"set", []string{"--assignment-epoch", "epoch:7"}, "epoch:7"},
		{"absent", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, _ := pinnedToolTree(t)
			configPath := mockConfig(t, dir, nil)

			status, stop := serve(t, configPath, filepath.Join(t.TempDir(), "memory.db"), "a1", tt.args...)
			require.NoError(t, stop())
			assert.Equal(t, "a1", status.AgentID)
			assert.Equal(t, tt.want, status.AssignmentEpoch)
			assert.Equal(t, tt.want, os.Getenv(tool.AssignmentEpochEnv),
				"the tools are handed the epoch the launcher gave")
		})
	}
}

// TestAgentCommand_GenerationIsNotAFlag holds contract v1.2: the
// generation is minted per start, so a launcher cannot pass one.
func TestAgentCommand_GenerationIsNotAFlag(t *testing.T) {
	err := runAgent(context.Background(), "--agent-id", "a1", "--generation", "3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown flag: --generation")
}

// generationShape is a runtime start nonce: 128 bits as lowercase hex.
var generationShape = regexp.MustCompile(`^[0-9a-f]{32}$`)

// TestAgentCommand_MintsTheGenerationOnlyUnderTheStoreLock holds the
// generation's ordering (contract v1.2): it is minted after the memory
// store's writer lock is held, so a start refused by a store another
// process holds hands its tools none. The control is the same start once
// the holder closes: it mints one, and hands the tools the generation
// its status reports.
func TestAgentCommand_MintsTheGenerationOnlyUnderTheStoreLock(t *testing.T) {
	dir, _ := pinnedToolTree(t)
	configPath := mockConfig(t, dir, nil)
	memoryPath := filepath.Join(t.TempDir(), "memory.db")

	held, err := sqlite.Open(memoryPath)
	require.NoError(t, err)
	err = runAgent(context.Background(), "--config", configPath, "--memory-path", memoryPath,
		"--addr", freeAddr(t), "--agent-id", "test-agent")
	assert.ErrorIs(t, err, sqlite.ErrStoreLocked)
	assert.Empty(t, os.Getenv(tool.GenerationEnv), "a start the store lock refused minted a generation")

	require.NoError(t, held.Close())
	status, stop := serve(t, configPath, memoryPath, "test-agent")
	assert.Equal(t, status.Generation, os.Getenv(tool.GenerationEnv),
		"the tools are handed the generation this start minted")
	require.NoError(t, stop(), "the control: the released store starts")
	assert.Regexp(t, generationShape, status.Generation)
}

// TestAgentCommand_EachStartMintsAFreshGeneration holds that the
// generation is per process start and never persisted (ADR 0016): two
// starts on the same memory store, one after the other, carry different
// 32-character lowercase hex generations.
func TestAgentCommand_EachStartMintsAFreshGeneration(t *testing.T) {
	dir, _ := pinnedToolTree(t)
	configPath := mockConfig(t, dir, nil)
	memoryPath := filepath.Join(t.TempDir(), "memory.db")

	generations := make([]string, 0, 2)
	for range 2 {
		status, stop := serve(t, configPath, memoryPath, "test-agent")
		require.NoError(t, stop(), "stopping releases the store for the next start")
		assert.Regexp(t, generationShape, status.Generation)
		generations = append(generations, status.Generation)
	}
	assert.NotEqual(t, generations[0], generations[1])
}

// TestAgentCommand_StatusReportsWhatThisStartLoaded holds C3's sources:
// the status route reports the agent ID and assignment epoch the flags
// gave, the generation this start minted, the revision the config file
// carries, and the process start time rather than the time the listener
// came up.
func TestAgentCommand_StatusReportsWhatThisStartLoaded(t *testing.T) {
	testStart := time.Now()
	dir, _ := pinnedToolTree(t)
	configPath := writeConfig(t, fmt.Sprintf("revision: rev-9\ntools-dir: %q\nmodel:\n  provider: mock\n", dir))

	status, stop := serve(t, configPath, filepath.Join(t.TempDir(), "memory.db"), "a1",
		"--assignment-epoch", "e3")
	require.NoError(t, stop())
	assert.Equal(t, "a1", status.AgentID)
	assert.Equal(t, "e3", status.AssignmentEpoch)
	assert.Equal(t, os.Getenv(tool.GenerationEnv), status.Generation)
	assert.Regexp(t, generationShape, status.Generation)
	assert.Equal(t, "rev-9", status.ConfigRevision)
	assert.True(t, status.StartedAt.Before(testStart),
		"started_at is the process start time, %s, which precedes this test's %s", status.StartedAt, testStart)
}
