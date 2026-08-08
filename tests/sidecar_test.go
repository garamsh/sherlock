package tests

import (
	"context"
	"encoding/json"
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

	"github.com/garamsh/gagent/internal/tool"
)

// TestEndToEnd_SidecarServesContract drives "gagent sidecar" as a
// subprocess — real binary, real HTTP, nothing mocked: the contract
// client discovers files, shell_exec, and the configured script tool and
// calls them over the wire (issue #204). When the binary cannot drop to
// the agent UID (the common CI case) shell_exec and the script tool
// report agent_uid unavailable; the test asserts that failure mode
// rather than skipping, so the wiring is exercised either way.
func TestEndToEnd_SidecarServesContract(t *testing.T) {
	binary := buildGagentBinary(t)
	workspace := filepath.Join(t.TempDir(), "workspace")
	configPath := writeSidecarConfig(t, "echo")
	cmd, baseURL, exited := startSidecarBinary(t, binary, workspace, configPath)

	descs, err := tool.Discover(context.Background(), baseURL)
	require.NoError(t, err)
	names := make([]string, 0, len(descs))
	byName := make(map[string]tool.Descriptor, len(descs))
	for _, d := range descs {
		names = append(names, d.Name)
		byName[d.Name] = d
	}
	assert.Equal(t, []string{"echo", "files", "shell_exec"}, names,
		"the sidecar serves the two native tools plus the configured script tool")

	filesTool := tool.NewRemoteTool(byName["files"], baseURL)
	_, err = tool.Invoke(context.Background(), filesTool,
		json.RawMessage(`{"operation":"write","path":"sub/hello.txt","content":"hi sidecar"}`))
	require.NoError(t, err)
	out, err := tool.Invoke(context.Background(), filesTool,
		json.RawMessage(`{"operation":"read","path":"sub/hello.txt"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"content":"hi sidecar"}`, string(out))
	content, err := os.ReadFile(filepath.Join(workspace, "sub", "hello.txt"))
	require.NoError(t, err, "the write lands in the sidecar's workspace directory")
	assert.Equal(t, "hi sidecar", string(content))

	_, err = tool.Invoke(context.Background(), filesTool,
		json.RawMessage(`{"operation":"read","path":"../escape"}`))
	require.ErrorIs(t, err, tool.ErrExecution)
	assert.Contains(t, err.Error(), "path escapes workspace",
		"workspace confinement holds across the wire")

	echoTool := tool.NewRemoteTool(byName["echo"], baseURL)
	out, err = tool.Invoke(context.Background(), echoTool, json.RawMessage(`{"ping":"pong"}`))
	if err != nil {
		assert.ErrorIs(t, err, tool.ErrExecution)
		assert.Contains(t, err.Error(), "agent_uid unavailable",
			"an unprivileged sidecar fails script-tool calls closed")
	} else {
		assert.JSONEq(t, `{"ping":"pong"}`, string(out),
			"cat echoes the tool input back on stdout")
	}

	// SIGTERM triggers the binary's graceful shutdown.
	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	select {
	case err := <-exited:
		require.NoError(t, err, "sidecar must exit cleanly on SIGTERM")
	case <-time.After(15 * time.Second):
		t.Fatal("sidecar did not exit on SIGTERM")
	}
}

// TestEndToEnd_SidecarInvalidToolFailsStartup runs the sidecar with a
// tools: entry named after the built-in files tool: it must exit
// non-zero at startup with an error naming the tool (issue #204).
func TestEndToEnd_SidecarInvalidToolFailsStartup(t *testing.T) {
	binary := buildGagentBinary(t)
	configPath := writeSidecarConfig(t, "files")

	cmd := exec.Command(binary, "sidecar",
		"--config", configPath, "--addr", "127.0.0.1:0",
		"--workspace", filepath.Join(t.TempDir(), "workspace"))
	out, err := cmd.CombinedOutput()
	require.Error(t, err, "gagent sidecar must fail startup on a colliding tools: entry")
	assert.Contains(t, string(out), `"files"`,
		"the startup error must name the offending tool")
}

// TestEndToEnd_SidecarNonLoopbackFailsStartup runs the sidecar with a
// non-loopback --addr: startup must fail with an error stating the
// loopback-only requirement — an unauthenticated contract listener is a
// remote shell into the computer (ADR 0013, issue #209).
func TestEndToEnd_SidecarNonLoopbackFailsStartup(t *testing.T) {
	binary := buildGagentBinary(t)
	configPath := emptyConfig(t, "sidecar.yaml")

	for _, addr := range []string{"0.0.0.0:8081", ":8081"} {
		cmd := exec.Command(binary, "sidecar",
			"--config", configPath, "--addr", addr,
			"--workspace", filepath.Join(t.TempDir(), "workspace"))
		out, err := cmd.CombinedOutput()
		require.Error(t, err, "addr %q must fail startup", addr)
		assert.Contains(t, string(out), "loopback")
		assert.Contains(t, string(out), "unauthenticated")
	}
}

// writeSidecarConfig writes a sidecar.yaml with one tools: entry of the
// given name (run through cat, echoing the JSON input back) plus the
// schema file it references, and returns the config path.
func writeSidecarConfig(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.json")
	require.NoError(t, os.WriteFile(schema, []byte(`{"type":"object"}`), 0o600))
	configPath := filepath.Join(dir, "sidecar.yaml")
	contents := "tools:\n" +
		"  - name: " + name + "\n" +
		"    description: A script tool.\n" +
		"    input_schema: " + schema + "\n" +
		"    output_schema: " + schema + "\n" +
		"    exec:\n" +
		"      command: [cat]\n" +
		"      stdout_format: json\n"
	require.NoError(t, os.WriteFile(configPath, []byte(contents), 0o600))
	return configPath
}

// startSidecarBinary starts "gagent sidecar" as a subprocess on a free
// loopback port with workspace as the workspace directory, waits until
// it serves the contract, and returns the process handle, the base URL,
// and a channel receiving the process's exit result. Test cleanup kills
// the process if it is still running.
func startSidecarBinary(t *testing.T, binary, workspace, configPath string) (*exec.Cmd, string, <-chan error) {
	t.Helper()

	listen, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listen.Addr().String()
	require.NoError(t, listen.Close())

	aux := t.TempDir()
	logPath := filepath.Join(aux, "sidecar.log")
	logFile, err := os.Create(logPath)
	require.NoError(t, err)
	t.Cleanup(func() { logFile.Close() })

	cmd := exec.Command(binary, "sidecar",
		"--config", configPath, "--addr", addr, "--workspace", workspace)
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
			t.Fatalf("sidecar exited before serving: %v\nlogs:\n%s", err, logs)
		default:
		}
		resp, err := http.Get(baseURL + "/tools")
		if err == nil {
			resp.Body.Close()
			return cmd, baseURL, exited
		}
		if time.Now().After(deadline) {
			logs, _ := os.ReadFile(logPath)
			t.Fatalf("sidecar did not serve within 10s\nlogs:\n%s", logs)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
