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
// serving the gRPC contract (files and exec) and the agent spawning
// the tools it loaded from its tools directory (ADR 0017). The mock
// model computes through shell_exec, which crosses the wire to the
// workspace's Exec RPC and runs in the workspace directory, reads its
// note back through the file_read tool, replies through message_send, and
// closes with a memory write.
func TestEndToEnd_BinaryMVPScenario(t *testing.T) {
	binary := buildGagentBinary(t)
	root := t.TempDir()

	// The workspace directory: the workspace serves it over gRPC and
	// execs children in it, so the file the exec'd child writes is the
	// file the file_read tool reads. The workspace creates the directory on
	// first use and the run writes its own content, so the test seeds
	// nothing.
	workspaceDir := filepath.Join(t.TempDir(), "workspace")

	_, workspaceAddr, _ := startWorkspaceBinary(t, binary,
		workspaceDir, emptyConfig(t, "workspace.yaml"))
	cmd, baseURL, exited := startAgentBinary(t, binary, root, workspaceAddr,
		emptyConfig(t, "gagent.yaml"))

	runBinaryMVPScenario(t, baseURL, root, binaryAgentID)

	// The exec'd child wrote into the workspace's own directory, where
	// the file_read tool read it back over the gRPC contract.
	written, err := os.ReadFile(filepath.Join(workspaceDir, "question.txt"))
	require.NoError(t, err)
	assert.Equal(t, "6 * 7\n", string(written))

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
// runMVPScenario. The canned loop drives the agent through shell_exec
// (writing a note into the workspace and computing through the
// workspace's Exec RPC), file_read (reading that note back), message_send,
// and a memory write.
func runBinaryMVPScenario(t *testing.T, baseURL, root, agentID string) {
	t.Helper()

	events := subscribeSSE(t, baseURL+"/agents/"+agentID+"/events")

	resp := postJSON(t, baseURL+"/agents/"+agentID+"/messages",
		`{"content":`+strconvQuote(e2eQuestion)+`}`)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	resp.Body.Close()

	// Four-step scenario: shell_exec, file_read, message_send, then the
	// final writeback — a writeback event follows every model response.
	got := collectEvents(t, events, 7)
	var kinds []string
	for _, e := range got {
		kinds = append(kinds, e["kind"].(string))
	}
	// The recall event is back: #630 wired the read step, so every turn
	// resolves the task into an address before the act loop (ADR 0019).
	// The writeback fires once per turn, when the turn ends on the
	// reserved call, rather than once per model response (issue #615).
	assert.Equal(t, []string{
		"turn_started", "recall",
		"tool_call", "tool_call", "tool_call",
		"writeback", "turn_finished",
	}, kinds)
	assert.Contains(t, got[0]["task"], "] unknown: "+e2eQuestion,
		"the turn input renders the message with its ID and sender (ADR 0015)")

	// shell_exec: the canned run's command crosses the Exec RPC, writes
	// its note into the workspace directory, and computes the answer.
	assert.Equal(t, "shell_exec", got[2]["tool_name"])
	assert.NotContains(t, got[2], "tool_failed")
	assert.Contains(t, got[2]["tool_result"], "42",
		"the canned run computes through the shell")

	// Files tool: the same file, read back through the gRPC contract —
	// the exec'd child and the file_read tool see one workspace directory.
	assert.Equal(t, "file_read", got[3]["tool_name"])
	assert.NotContains(t, got[3], "tool_failed")
	assert.Contains(t, got[3]["tool_result"], "6 * 7",
		"the file_read tool reads back what the exec'd child wrote, through the workspace gRPC client")

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
// --message" against a running workspace: the one-shot mode runs the
// canned turn — shell_exec, file_read, message_send via the protocol
// client, plus a memory write — and exits. The outbound message
// lands in the file outbox (the same dir the serving mode uses); the
// one-shot runner echoes it to stdout after the turn lands.
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
	// The script-tool registry reads ./tools by default; the in-tree
	// location `make build-tools` populates in place, so point the env
	// at the repo root's tools/ dir explicitly. The github manifest
	// also requires GH_TOKEN at startup, even though this scenario
	// never invokes github.
	toolsDir := toolsDirForTest(t)
	cmd.Env = append(os.Environ(),
		"GAGENT_TOOLS_DIR="+toolsDir,
		"GH_TOKEN=fake-test-token",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "one-shot agent turn must exit cleanly: %s", out)
	assert.Contains(t, string(out), "Replied via message_send",
		"the final response prints to stdout")
	assert.Contains(t, string(out), "[http -> sender] 42",
		"one-shot mode echoes outbound messages to stdout after the turn lands")

	// The message_send binary writes to the file outbox; the
	// one-shot mode shares the same directory the serving mode uses.
	require.Eventually(t, func() bool {
		return len(outboxMessages(t, root)) == 1
	}, 5*time.Second, 10*time.Millisecond)
	replies := outboxMessages(t, root)
	require.Len(t, replies, 1)
	assert.Equal(t, "42", replies[0].Content)
	assert.Equal(t, "http", replies[0].Channel)
	assert.Equal(t, "sender", replies[0].Target)
}

// The next two tests are a pair, and the pair is the point: the same
// broken deployment — an agent whose workspace is not listening, so
// shell_exec and file_read fail at the boundary — is run down each of the
// agent's two paths, and they are meant to end differently.
//
// A served turn hands the failure to the model, which gets another
// iteration to react; the turn finishes and the gateway keeps serving.
// A one-shot run has no model left to react and no caller still
// watching the stream, so it reports the failure the only way a process
// that has already exited can: its exit status.
//
// Neither path branches on a mode, a flag, or the configured provider.
// They differ because serving a gateway and running one turn are
// different things (docs/architecture/gateway.md).

// TestEndToEnd_OneShotAgentTurnFailedToolCallExitsNonZero is the
// one-shot half of that pair. The canned run still reaches
// message_send and still finishes — a failed tool call is not a failed
// turn — so the turn's own result cannot carry the failure, and
// exiting 0 here would report a clean run that did not happen.
func TestEndToEnd_OneShotAgentTurnFailedToolCallExitsNonZero(t *testing.T) {
	binary := buildGagentBinary(t)
	root := t.TempDir()

	cmd := exec.Command(binary, "agent",
		"--config", emptyConfig(t, "gagent.yaml"),
		"--agent-id", binaryAgentID,
		"--memory-path", filepath.Join(root, "memory.db"),
		"--workspace-addr", closedLoopbackAddr(t),
		"--message", e2eQuestion)
	cmd.Env = append(os.Environ(),
		"GAGENT_TOOLS_DIR="+toolsDirForTest(t),
		"GH_TOKEN=fake-test-token",
	)
	out, err := cmd.CombinedOutput()

	require.Error(t, err, "a turn whose tool calls failed must not exit 0: %s", out)
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.NotZero(t, exitErr.ExitCode())
	assert.Contains(t, string(out), "tool call(s) failed",
		"the run names what failed rather than only setting a status")
	for _, name := range []string{"shell_exec", "file_read"} {
		assert.Contains(t, string(out), name,
			"the run names every tool that failed at the workspace boundary")
	}

	// The turn itself still ran: the reply the canned scenario sends
	// through message_send is on stdout and in the outbox, because the
	// failure is reported, not thrown away.
	assert.Contains(t, string(out), "[http -> sender] 42",
		"the turn's output still prints before the failure is reported")
	replies := outboxMessages(t, root)
	require.Len(t, replies, 1)
	assert.Equal(t, "42", replies[0].Content)
}

// TestEndToEnd_ServedTurnFailedToolCallCarriesOn is the serving half.
// The same failing tool calls reach the model as tool results, the
// turn ends in turn_finished rather than turn_failed, and the gateway
// accepts the next message — a model recovering from a tool error is
// the loop working, and this path must not acquire the one-shot's
// exit contract.
func TestEndToEnd_ServedTurnFailedToolCallCarriesOn(t *testing.T) {
	binary := buildGagentBinary(t)
	root := t.TempDir()

	cmd, baseURL, exited := startAgentBinary(t, binary, root, closedLoopbackAddr(t),
		emptyConfig(t, "gagent.yaml"))

	events := subscribeSSE(t, baseURL+"/agents/"+binaryAgentID+"/events")
	resp := postJSON(t, baseURL+"/agents/"+binaryAgentID+"/messages",
		`{"content":`+strconvQuote(e2eQuestion)+`}`)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	resp.Body.Close()

	got := collectEvents(t, events, 7)
	var kinds []string
	for _, e := range got {
		kinds = append(kinds, e["kind"].(string))
	}
	assert.Equal(t, []string{
		"turn_started", "recall",
		"tool_call", "tool_call", "tool_call",
		"writeback", "turn_finished",
	}, kinds, "the turn runs to completion despite the failures")

	// The two tools that cross to the workspace failed, and each
	// failure was fed back to the model as a tool result.
	assert.Equal(t, "shell_exec", got[2]["tool_name"])
	assert.Equal(t, true, got[2]["tool_failed"])
	assert.Equal(t, "file_read", got[3]["tool_name"])
	assert.Equal(t, true, got[3]["tool_failed"])

	// message_send needs no workspace, so the mouth still worked and
	// the turn ended cleanly rather than failing.
	assert.Equal(t, "message_send", got[4]["tool_name"])
	assert.NotContains(t, got[4], "tool_failed")
	assert.Equal(t, "turn_finished", got[6]["kind"],
		"a failed tool call never terminates the turn as turn_failed")

	// The agent is still serving: the failures did not take the
	// process down the one-shot's path.
	select {
	case err := <-exited:
		t.Fatalf("the agent exited after a failed tool call: %v", err)
	default:
	}
	second := postJSON(t, baseURL+"/agents/"+binaryAgentID+"/messages",
		`{"content":`+strconvQuote(e2eQuestion)+`}`)
	assert.Equal(t, http.StatusAccepted, second.StatusCode,
		"the gateway still accepts messages after a turn with failed tool calls")
	second.Body.Close()

	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	select {
	case err := <-exited:
		require.NoError(t, err, "the served agent still exits cleanly on SIGTERM")
	case <-time.After(15 * time.Second):
		t.Fatal("agent did not exit on SIGTERM")
	}
}

// closedLoopbackAddr returns a loopback address with nothing listening
// on it: the port is bound to find a free one and released before the
// address is handed back. Dialing it is what makes the workspace-bound
// tools fail at their boundary.
func closedLoopbackAddr(t *testing.T) string {
	t.Helper()
	listen, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listen.Addr().String()
	require.NoError(t, listen.Close())
	return addr
}

// buildGagentBinary compiles the gagent CLI binary into a temp
// directory and builds each first-party tool into the module's
// tools/<name>/<name> so the script-tool registry can resolve it
// through GAGENT_TOOLS_DIR. The six tools are built in place (the
// same shape `make build-tools` produces), and the agent binary is
// the only thing copied to the tempdir; the test sets GAGENT_TOOLS_DIR
// to the module root's tools/ dir so the run doesn't depend on
// `go install`.
func buildGagentBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(dir, "gagent"), "./cmd/gagent")
	build.Dir = ".." // the module root; the test binary runs in tests/
	out, err := build.CombinedOutput()
	require.NoError(t, err, "go build failed: %s", out)

	toolsDir := toolsDirForTest(t)
	for _, tool := range []string{"file_read", "file_write", "file_list", "shell_exec", "message_send", "github"} {
		toolBuild := exec.Command("go", "build", "-o",
			filepath.Join(toolsDir, tool, tool), "./tools/"+tool)
		toolBuild.Dir = ".." // the module root
		if toolOut, err := toolBuild.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", tool, err, toolOut)
		}
	}
	return filepath.Join(dir, "gagent")
}

// emptyConfig writes an empty config file — it keeps a binary run
// hermetic against ./configs/gagent.yaml, ./configs/workspace.yaml, and XDG config
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
// model provider configPath selects — an empty config file leaves the
// default mock. The agent executes nothing itself — the workspace runs
// shell_exec children in single-account mode (startWorkspaceBinary
// passes the test process's own UID). It waits until the agent serves
// HTTP and returns the process handle, the base URL, and a channel
// receiving the process's exit result. Test cleanup kills the process
// if it is still running.
func startAgentBinary(t *testing.T, binary, root, workspaceAddr, configPath string) (*exec.Cmd, string, <-chan error) {
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
		"--config", configPath,
		"--addr", addr,
		"--agent-id", binaryAgentID,
		"--memory-path", filepath.Join(root, "memory.db"),
		"--workspace-addr", workspaceAddr)
	// The script-tool registry reads ./tools; the in-tree location
	// `make build-tools` populates in place, so point the env at the
	// repo root's tools/ dir explicitly.
	toolsDir := toolsDirForTest(t)
	cmd.Env = append(os.Environ(),
		"GAGENT_TOOLS_DIR="+toolsDir,
		"GH_TOKEN=fake-test-token",
	)
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

// toolsDirForTest returns the absolute path to the module-root tools/
// directory so subprocess-driven test runs find the shipped tools'
// manifests regardless of cwd.
func toolsDirForTest(t *testing.T) string {
	t.Helper()
	return filepath.Join(moduleRoot(t), "tools")
}

// moduleRoot returns the directory holding go.mod. The test process
// chdir's around, so walking up from the working directory is the only
// reliable way to reach a path stated relative to the module.
func moduleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			t.Fatalf("could not locate module root from %s", wd)
		}
		wd = parent
	}
}
