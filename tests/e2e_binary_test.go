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

// binaryEnvID is this scenario's own environment ID.
const binaryEnvID = "e2e-bin"

// TestEndToEnd_BinaryMVPScenario drives the canned MVP scenario against the
// built gagent binary served as a subprocess — the e2e layer of
// docs/convention/testing.md: real binary, real HTTP, nothing mocked beyond
// the scripted mock model provider.
//
// After ADR 0012 the canned script calls shell_exec, which the real binary
// runs through ProcessExecutor. When the binary is not running as root
// (the common CI case) shell_exec returns agent_uid unavailable; the test
// asserts that failure mode rather than skipping the scenario, so the
// binary wiring is exercised end-to-end either way.
func TestEndToEnd_BinaryMVPScenario(t *testing.T) {
	binary := buildGagentBinary(t)
	root := t.TempDir()
	cmd, baseURL, exited := startGatewayBinary(t, binary, root)

	runBinaryMVPScenario(t, baseURL, root, binaryEnvID)

	// SIGTERM triggers the binary's graceful shutdown.
	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	select {
	case err := <-exited:
		require.NoError(t, err, "gateway must exit cleanly on SIGTERM")
	case <-time.After(15 * time.Second):
		t.Fatal("gateway did not exit on SIGTERM")
	}

	// After the binary exits, the canned memory write is still queryable
	// from the environment's memory store on disk.
	assertMemoryStore(t, filepath.Join(root, binaryEnvID))
}

// runBinaryMVPScenario is the binary test's variant of runMVPScenario.
// It exercises the same canned loop but tolerates the shell_exec call
// failing with agent_uid unavailable when the binary cannot drop to the
// agent UID (typical CI environment without setuid-root). The in-process
// variant in e2e_test.go uses a fake executor so shell_exec always
// succeeds there.
func runBinaryMVPScenario(t *testing.T, baseURL, root, envID string) {
	t.Helper()

	events := subscribeSSE(t, baseURL+"/envs/"+envID+"/events")

	resp := postJSON(t, baseURL+"/envs/"+envID+"/messages",
		`{"content":`+strconvQuote(e2eQuestion)+`}`)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	resp.Body.Close()

	got := collectEvents(t, events, 9)
	var kinds []string
	for _, e := range got {
		kinds = append(kinds, e["kind"].(string))
	}
	assert.Equal(t, []string{
		"turn_started",
		"writeback", "tool_call",
		"writeback", "tool_call",
		"writeback", "tool_call",
		"writeback", "turn_finished",
	}, kinds)
	assert.Equal(t, e2eQuestion, got[0]["task"])

	assert.Equal(t, "memory", got[2]["tool_name"])
	assert.NotContains(t, got[2], "tool_failed")
	assert.Equal(t, "shell_exec", got[4]["tool_name"])
	// shell_exec fails when the binary cannot drop privileges; assert
	// the failure mode is the documented agent_uid unavailable rather
	// than masking it.
	if failed, _ := got[4]["tool_failed"].(bool); failed {
		result, _ := got[4]["tool_result"].(string)
		assert.Contains(t, result, "agent_uid unavailable",
			"shell_exec failure mode in the binary test must surface as agent_uid unavailable")
	}
	assert.Equal(t, "message_send", got[6]["tool_name"])
	assert.NotContains(t, got[6], "tool_failed")

	require.Eventually(t, func() bool {
		return len(outboxMessages(t, root, envID)) == 1
	}, 5*time.Second, 10*time.Millisecond)
	replies := outboxMessages(t, root, envID)
	require.Len(t, replies, 1)
	assert.Equal(t, "42", replies[0].Content)
	assert.Equal(t, "http", replies[0].Channel)
	assert.Equal(t, "sender", replies[0].Target)
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

// startGatewayBinary starts "gagent gateway" as a subprocess on a free
// loopback port with root as the data root and the default mock model
// provider, waits until it serves HTTP, and returns the process handle,
// the base URL, and a channel receiving the process's exit result. An
// explicit empty config file keeps the run hermetic against ./gagent.yaml
// and XDG config files on the host. Test cleanup kills the process if it
// is still running.
func startGatewayBinary(t *testing.T, binary, root string) (*exec.Cmd, string, <-chan error) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "gagent.yaml")
	require.NoError(t, os.WriteFile(configPath, nil, 0o600))
	return startGatewayWithConfig(t, binary, root, configPath)
}

// startGatewayWithConfig is startGatewayBinary with a caller-supplied
// config file.
func startGatewayWithConfig(t *testing.T, binary, root, configPath string) (*exec.Cmd, string, <-chan error) {
	t.Helper()

	listen, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listen.Addr().String()
	require.NoError(t, listen.Close())

	aux := t.TempDir()
	logPath := filepath.Join(aux, "gateway.log")
	logFile, err := os.Create(logPath)
	require.NoError(t, err)
	t.Cleanup(func() { logFile.Close() })

	cmd := exec.Command(binary, "gateway",
		"--config", configPath, "--addr", addr, "--data-root", root)
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
	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case err := <-exited:
			logs, _ := os.ReadFile(logPath)
			t.Fatalf("gateway exited before serving: %v\nlogs:\n%s", err, logs)
		default:
		}
		resp, err := http.Get(baseURL + "/")
		if err == nil {
			resp.Body.Close()
			return cmd, baseURL, exited
		}
		if time.Now().After(deadline) {
			logs, _ := os.ReadFile(logPath)
			t.Fatalf("gateway did not serve within 10s\nlogs:\n%s", logs)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestEndToEnd_ScriptToolCollisionFailsStartup runs both entry points
// with a tools: entry named after the built-in memory tool (issue #124):
// each must exit non-zero at startup with an error naming the tool,
// instead of serving and surfacing the error on the first message.
func TestEndToEnd_ScriptToolCollisionFailsStartup(t *testing.T) {
	binary := buildGagentBinary(t)
	root := t.TempDir()
	configPath := writeScriptToolsConfig(t, "memory")

	gatewayCmd := exec.Command(binary, "gateway",
		"--config", configPath, "--addr", "127.0.0.1:0", "--data-root", root)
	out, err := gatewayCmd.CombinedOutput()
	require.Error(t, err, "gagent gateway must fail startup on a colliding tools: entry")
	assert.Contains(t, string(out), `"memory"`,
		"the startup error must name the offending tool")

	agentCmd := exec.Command(binary, "agent",
		"--config", configPath, "--data-root", root, "--message", "hi")
	out, err = agentCmd.CombinedOutput()
	require.Error(t, err, "gagent agent must fail startup on a colliding tools: entry")
	assert.Contains(t, string(out), `"memory"`,
		"the startup error must name the offending tool")
}

// TestEndToEnd_ValidScriptToolStartsAndServes is the regression half of
// issue #124: a valid tools: entry must not stop the gateway from
// starting and serving.
func TestEndToEnd_ValidScriptToolStartsAndServes(t *testing.T) {
	binary := buildGagentBinary(t)
	root := t.TempDir()
	configPath := writeScriptToolsConfig(t, "wordcount")

	_, baseURL, _ := startGatewayWithConfig(t, binary, root, configPath)

	resp, err := http.Get(baseURL + "/")
	require.NoError(t, err)
	resp.Body.Close()
}

// writeScriptToolsConfig writes a gagent.yaml with one tools: entry of
// the given name plus the schema files it references, and returns the
// config path.
func writeScriptToolsConfig(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.json")
	require.NoError(t, os.WriteFile(schema, []byte(`{"type":"object"}`), 0o600))
	configPath := filepath.Join(dir, "gagent.yaml")
	contents := "tools:\n" +
		"  - name: " + name + "\n" +
		"    description: A script tool.\n" +
		"    input_schema: " + schema + "\n" +
		"    output_schema: " + schema + "\n" +
		"    exec:\n" +
		"      command: [true]\n"
	require.NoError(t, os.WriteFile(configPath, []byte(contents), 0o600))
	return configPath
}
