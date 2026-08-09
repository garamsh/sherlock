package tests

import (
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// binaryAgentID is this scenario's own agent ID.
const binaryAgentID = "e2e-bin"

// TestEndToEnd_BinaryMVPScenario drives the canned MVP scenario against
// the built gagent binary run as two processes — the workspace serving
// the workspace over the tool contract and the agent discovering it at
// startup (ADR 0018). This is the e2e layer of
// docs/convention/testing.md: real binaries, real HTTP, nothing mocked
// beyond the scripted mock model provider.
//
// When the workspace cannot drop to the agent UID (the common CI case)
// shell_exec returns agent_uid unavailable; the test asserts that
// failure mode rather than skipping the scenario, so the wiring is
// exercised end-to-end either way.
func TestEndToEnd_BinaryMVPScenario(t *testing.T) {
	binary := buildGagentBinary(t)
	root := t.TempDir()

	_, workspaceURL, _ := startWorkspaceBinary(t, binary,
		filepath.Join(t.TempDir(), "workspace"), emptyConfig(t, "workspace.yaml"))
	cmd, baseURL, exited := startAgentBinary(t, binary, root, workspaceURL)

	runBinaryMVPScenario(t, baseURL, root, binaryAgentID)

	// SIGTERM triggers the agent's graceful shutdown.
	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	select {
	case err := <-exited:
		require.NoError(t, err, "agent must exit cleanly on SIGTERM")
	case <-time.After(15 * time.Second):
		t.Fatal("agent did not exit on SIGTERM")
	}

	// After the agent exits, the canned memory write is still queryable
	// from the memory store on disk.
	assertMemoryStore(t, root)
}

// runBinaryMVPScenario is the binary test's variant of runMVPScenario.
// It exercises the same canned loop but tolerates the shell_exec call
// failing with agent_uid unavailable when the workspace cannot drop to
// the agent UID (typical CI environment without setuid-root). The
// in-process variant in e2e_test.go serves a canned computer tool so
// shell_exec always succeeds there.
func runBinaryMVPScenario(t *testing.T, baseURL, root, agentID string) {
	t.Helper()

	events := subscribeSSE(t, baseURL+"/agents/"+agentID+"/events")

	resp := postJSON(t, baseURL+"/agents/"+agentID+"/messages",
		`{"content":`+strconvQuote(e2eQuestion)+`}`)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	resp.Body.Close()

	got := collectEvents(t, events, 7)
	var kinds []string
	for _, e := range got {
		kinds = append(kinds, e["kind"].(string))
	}
	assert.Equal(t, []string{
		"turn_started",
		"writeback", "tool_call",
		"writeback", "tool_call",
		"writeback", "turn_finished",
	}, kinds)
	assert.Contains(t, got[0]["task"], "] unknown: "+e2eQuestion,
		"the turn input renders the message with its ID and sender (ADR 0016)")

	assert.Equal(t, "shell_exec", got[2]["tool_name"])
	// shell_exec fails when the workspace cannot drop privileges; assert
	// the failure mode is the documented agent_uid unavailable rather
	// than masking it.
	if failed, _ := got[2]["tool_failed"].(bool); failed {
		result, _ := got[2]["tool_result"].(string)
		assert.Contains(t, result, "agent_uid unavailable",
			"shell_exec failure mode in the binary test must surface as agent_uid unavailable")
	}
	assert.Equal(t, "message_send", got[4]["tool_name"])
	assert.NotContains(t, got[4], "tool_failed")

	require.Eventually(t, func() bool {
		return len(outboxMessages(t, root)) == 1
	}, 5*time.Second, 10*time.Millisecond)
	replies := outboxMessages(t, root)
	require.Len(t, replies, 1)
	assert.Equal(t, "42", replies[0].Content)
	assert.Equal(t, "http", replies[0].Channel)
	assert.Equal(t, "sender", replies[0].Target)
}

// TestEndToEnd_OneShotAgentTurnAgainstWorkspace runs "gagent agent
// --message" against a running workspace: the one-shot mode discovers
// the same tool set, runs the full canned turn — discovered shell_exec,
// message_send, writeback — prints the reply to stdout, and exits.
func TestEndToEnd_OneShotAgentTurnAgainstWorkspace(t *testing.T) {
	binary := buildGagentBinary(t)
	root := t.TempDir()

	_, workspaceURL, _ := startWorkspaceBinary(t, binary,
		filepath.Join(t.TempDir(), "workspace"), emptyConfig(t, "workspace.yaml"))

	cmd := exec.Command(binary, "agent",
		"--config", emptyConfig(t, "gagent.yaml"),
		"--agent-id", binaryAgentID,
		"--memory-path", filepath.Join(root, "memory.db"),
		"--tool-servers", workspaceURL,
		"--message", e2eQuestion)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "one-shot agent turn must exit cleanly: %s", out)
	assert.Contains(t, string(out), "[http -> sender] 42",
		"message_send output prints to stdout")
	assert.Contains(t, string(out), "Replied via message_send",
		"the final response prints to stdout")
}

// TestEndToEnd_UnreachableToolServerFailsStartup pins the ADR 0018
// discovery contract: a configured tool server that never comes up fails
// agent startup with an error naming it, after bounded retries.
func TestEndToEnd_UnreachableToolServerFailsStartup(t *testing.T) {
	binary := buildGagentBinary(t)
	root := t.TempDir()

	// A listener that closes immediately leaves a port nothing serves.
	listen, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	dead := "http://" + listen.Addr().String()
	require.NoError(t, listen.Close())

	cmd := exec.Command(binary, "agent",
		"--config", emptyConfig(t, "gagent.yaml"),
		"--agent-id", binaryAgentID,
		"--memory-path", filepath.Join(root, "memory.db"),
		"--tool-servers", dead,
		"--message", "hi")
	out, err := cmd.CombinedOutput()
	require.Error(t, err, "an unreachable tool server must fail startup")
	assert.Contains(t, string(out), dead,
		"the startup error must name the unreachable server")
}

// buildGagentBinary compiles the gagent CLI into a temp path and returns
// the binary's path.
func buildGagentBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gagent")
	build := exec.Command("go", "build", "-o", path, "./cmd/gagent")
	build.Dir = ".." // the module root; the test binary runs in tests/
	out, err := build.CombinedOutput()
	require.NoError(t, err, "go build failed: %s", out)
	return path
}

// emptyConfig writes an empty config file — it keeps a binary run
// hermetic against ./gagent.yaml, ./workspace.yaml, and XDG config
// files on the host — and returns its path.
func emptyConfig(t *testing.T, name string) string {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(configPath, nil, 0o600))
	return configPath
}

// startAgentBinary starts "gagent agent" as a subprocess on a free
// loopback port, discovering toolServers, with root holding the memory
// file and the outbox, and the default mock model provider. It waits
// until the agent serves HTTP and returns the process handle, the base
// URL, and a channel receiving the process's exit result. Test cleanup
// kills the process if it is still running.
func startAgentBinary(t *testing.T, binary, root, toolServers string) (*exec.Cmd, string, <-chan error) {
	t.Helper()

	listen, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listen.Addr().String()
	require.NoError(t, listen.Close())

	aux := t.TempDir()
	logPath := filepath.Join(aux, "agent.log")
	logFile, err := os.Create(logPath)
	require.NoError(t, err)
	t.Cleanup(func() { logFile.Close() })

	cmd := exec.Command(binary, "agent",
		"--config", emptyConfig(t, "gagent.yaml"),
		"--addr", addr,
		"--agent-id", binaryAgentID,
		"--memory-path", filepath.Join(root, "memory.db"),
		"--tool-servers", toolServers)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	require.NoError(t, cmd.Start())
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			<-exited
		}
	})

	baseURL := "http://" + addr
	deadline := time.Now().Add(15 * time.Second)
	for {
		select {
		case err := <-exited:
			logs, _ := os.ReadFile(logPath)
			t.Fatalf("agent exited before serving: %v\nlogs:\n%s", err, logs)
		default:
		}
		resp, err := http.Get(baseURL + "/")
		if err == nil {
			resp.Body.Close()
			return cmd, baseURL, exited
		}
		if time.Now().After(deadline) {
			logs, _ := os.ReadFile(logPath)
			t.Fatalf("agent did not serve within 15s\nlogs:\n%s", logs)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
