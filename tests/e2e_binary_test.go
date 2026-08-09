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

// TestEndToEnd_BinaryMVPScenario drives the canned MVP scenario
// against the built gagent binary run as two processes — the workspace
// serving the filesystem gRPC contract and the agent importing tools
// at compile time (ADR 0018). The mock model uses the tools the agent
// hosts (no shell_exec): message_send plus a writeback block.
func TestEndToEnd_BinaryMVPScenario(t *testing.T) {
	binary := buildGagentBinary(t)
	root := t.TempDir()

	_, workspaceAddr, _ := startWorkspaceBinary(t, binary,
		filepath.Join(t.TempDir(), "workspace"), emptyConfig(t, "workspace.yaml"))
	cmd, baseURL, exited := startAgentBinary(t, binary, root, workspaceAddr)

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

// runBinaryMVPScenario is the binary test's variant of
// runMVPScenario. The canned loop drives the agent through
// message_send (the only tool the mock model invokes) and a writeback
// block.
func runBinaryMVPScenario(t *testing.T, baseURL, root, agentID string) {
	t.Helper()

	events := subscribeSSE(t, baseURL+"/agents/"+agentID+"/events")

	resp := postJSON(t, baseURL+"/agents/"+agentID+"/messages",
		`{"content":`+strconvQuote(e2eQuestion)+`}`)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	resp.Body.Close()

	// Two-step scenario now: message_send, then the final writeback.
	got := collectEvents(t, events, 5)
	var kinds []string
	for _, e := range got {
		kinds = append(kinds, e["kind"].(string))
	}
	assert.Equal(t, []string{
		"turn_started",
		"writeback", "tool_call",
		"writeback", "turn_finished",
	}, kinds)
	assert.Contains(t, got[0]["task"], "] unknown: "+e2eQuestion,
		"the turn input renders the message with its ID and sender (ADR 0016)")

	assert.Equal(t, "message_send", got[2]["tool_name"])
	assert.NotContains(t, got[2], "tool_failed")

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
// --message" against a running workspace: the one-shot mode uses the
// same compile-time tool set, runs the canned turn — message_send
// plus a writeback block — prints the reply to stdout, and exits.
func TestEndToEnd_OneShotAgentTurnAgainstWorkspace(t *testing.T) {
	binary := buildGagentBinary(t)
	root := t.TempDir()

	_, workspaceAddr, _ := startWorkspaceBinary(t, binary,
		filepath.Join(t.TempDir(), "workspace"), emptyConfig(t, "workspace.yaml"))

	cmd := exec.Command(binary, "agent",
		"--config", emptyConfig(t, "gagent.yaml"),
		"--agent-id", binaryAgentID,
		"--memory-path", filepath.Join(root, "memory.db"),
		"--workspace-addr", workspaceAddr,
		"--message", e2eQuestion)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "one-shot agent turn must exit cleanly: %s", out)
	assert.Contains(t, string(out), "[http -> sender] 42",
		"message_send output prints to stdout")
	assert.Contains(t, string(out), "Replied via message_send",
		"the final response prints to stdout")
}

// buildGagentBinary compiles the gagent CLI into a temp path and
// returns the binary's path.
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
// loopback port, dialing workspaceAddr as the agent's workspace gRPC
// endpoint, with root holding the memory file and the outbox, and the
// default mock model provider. It waits until the agent serves HTTP
// and returns the process handle, the base URL, and a channel
// receiving the process's exit result. Test cleanup kills the process
// if it is still running.
func startAgentBinary(t *testing.T, binary, root, workspaceAddr string) (*exec.Cmd, string, <-chan error) {
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
		"--workspace-addr", workspaceAddr)
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
