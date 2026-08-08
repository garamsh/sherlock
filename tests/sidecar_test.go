package tests

import (
	"context"
	"encoding/json"
	"fmt"
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

// TestEndToEnd_SidecarSingleAccountMode runs the sidecar as the test's
// own unprivileged user with --exec-uid set to that UID (ADR 0008's
// single-account mode): shell_exec and the script tool run for real —
// no agent_uid unavailable — and the shell_exec child stays
// network-isolated through the unprivileged user+network namespace.
// Verified on a uid-1000 host without sudo.
func TestEndToEnd_SidecarSingleAccountMode(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("single-account mode is only meaningful without root")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH; the network probe needs it")
	}
	binary := buildGagentBinary(t)
	workspace := filepath.Join(t.TempDir(), "workspace")
	configPath := writeSidecarConfig(t, "echo")

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
		"--config", configPath, "--addr", addr, "--workspace", workspace,
		"--exec-uid", fmt.Sprint(os.Geteuid()))
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
	var descs []tool.Descriptor
	require.Eventually(t, func() bool {
		descs, err = tool.Discover(context.Background(), baseURL)
		return err == nil
	}, 10*time.Second, 20*time.Millisecond, "the sidecar must serve the contract\nlogs:\n%s", readLogs(logPath))
	byName := make(map[string]tool.Descriptor, len(descs))
	for _, d := range descs {
		byName[d.Name] = d
	}

	// shell_exec runs for real as the sidecar's own account.
	shellTool := tool.NewRemoteTool(byName["shell_exec"], baseURL)
	out, err := tool.Invoke(context.Background(), shellTool,
		json.RawMessage(`{"command":"echo -n $(id -u):$(id -g)"}`))
	require.NoError(t, err, "single-account shell_exec must not report agent_uid unavailable\nlogs:\n%s", readLogs(logPath))
	assert.Contains(t, string(out), fmt.Sprintf(`"stdout":"%d:%d"`, os.Geteuid(), os.Getegid()))

	// The shell_exec child stays network-isolated: it sees only the
	// loopback interface.
	out, err = tool.Invoke(context.Background(), shellTool,
		json.RawMessage(`{"command":"python3 -c \"import socket; print(socket.if_nameindex())\""}`))
	require.NoError(t, err)
	assert.Contains(t, string(out), `[(1, 'lo')]`)

	// The script tool runs and keeps network access (ADR 0010's
	// operator-sanctioned egress channel).
	echoTool := tool.NewRemoteTool(byName["echo"], baseURL)
	out, err = tool.Invoke(context.Background(), echoTool, json.RawMessage(`{"ping":"pong"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"ping":"pong"}`, string(out),
		"cat echoes the tool input back on stdout in single-account mode")

	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	select {
	case err := <-exited:
		require.NoError(t, err, "sidecar must exit cleanly on SIGTERM")
	case <-time.After(15 * time.Second):
		t.Fatal("sidecar did not exit on SIGTERM")
	}
}

// TestEndToEnd_SidecarScriptToolRunsGoBinary is the issue #225
// acceptance case at the wire: a Go-compiled binary — whose runtime
// reserves more virtual address space than the shell's adversarial
// 512MiB cap — works as a script tool, because script tools run in the
// trusted class (ADR 0010), bounded by the per-call timeout rather
// than the shell rlimits.
func TestEndToEnd_SidecarScriptToolRunsGoBinary(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the trusted-class executor path is exercised unprivileged here")
	}
	binary := buildGagentBinary(t)
	helper := buildGoScriptHelper(t)
	workspace := filepath.Join(t.TempDir(), "workspace")

	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.json")
	require.NoError(t, os.WriteFile(schema, []byte(`{"type":"object"}`), 0o600))
	configPath := filepath.Join(dir, "sidecar.yaml")
	contents := "tools:\n" +
		"  - name: go-helper\n" +
		"    description: A Go-compiled script tool.\n" +
		"    input_schema: " + schema + "\n" +
		"    output_schema: " + schema + "\n" +
		"    exec:\n" +
		"      command: [" + helper + "]\n"
	require.NoError(t, os.WriteFile(configPath, []byte(contents), 0o600))

	cmd, baseURL, exited := startSidecarWith(t, binary, configPath, workspace,
		"--exec-uid", fmt.Sprint(os.Geteuid()))
	defer stopSidecar(t, cmd, exited)

	descs, err := tool.Discover(context.Background(), baseURL)
	require.NoError(t, err)
	var goHelper tool.Descriptor
	for _, d := range descs {
		if d.Name == "go-helper" {
			goHelper = d
		}
	}
	require.NotNil(t, goHelper)

	out, err := tool.Invoke(context.Background(),
		tool.NewRemoteTool(goHelper, baseURL), json.RawMessage(`{}`))
	require.NoError(t, err, "the Go binary must run as a script tool, not die under the shell rlimits")
	assert.Equal(t, "go-helper-ok", string(out))
}

// buildGoScriptHelper compiles a trivial Go binary for the
// script-tool acceptance case.
func buildGoScriptHelper(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	main := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(main, []byte(
		"package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Print(\"go-helper-ok\") }\n"), 0o644))
	out := filepath.Join(dir, "helper")
	cmd := exec.Command("go", "build", "-o", out, main)
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		t.Skipf("go toolchain unavailable for the helper build: %v", err)
	}
	return out
}

// startSidecarWith is startSidecarBinary with extra arguments (e.g.
// --exec-uid) appended.
func startSidecarWith(t *testing.T, binary, configPath, workspace string, extraArgs ...string) (*exec.Cmd, string, <-chan error) {
	t.Helper()

	listen, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listen.Addr().String()
	require.NoError(t, listen.Close())

	logPath := filepath.Join(t.TempDir(), "sidecar.log")
	logFile, err := os.Create(logPath)
	require.NoError(t, err)
	t.Cleanup(func() { logFile.Close() })

	args := append([]string{"sidecar", "--config", configPath, "--addr", addr, "--workspace", workspace}, extraArgs...)
	cmd := exec.Command(binary, args...)
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
	require.Eventually(t, func() bool {
		_, err := tool.Discover(context.Background(), baseURL)
		return err == nil
	}, 10*time.Second, 20*time.Millisecond, "the sidecar must serve the contract\nlogs:\n%s", readLogs(logPath))
	return cmd, baseURL, exited
}

// stopSidecar SIGTERMs the sidecar and requires a clean exit.
func stopSidecar(t *testing.T, cmd *exec.Cmd, exited <-chan error) {
	t.Helper()
	if cmd.ProcessState != nil {
		return
	}
	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	select {
	case err := <-exited:
		require.NoError(t, err, "sidecar must exit cleanly on SIGTERM")
	case <-time.After(15 * time.Second):
		t.Fatal("sidecar did not exit on SIGTERM")
	}
}

// TestEndToEnd_SidecarSingleAccountSecretCustody pins the ADR 0003
// custody story for single-account mode (issue #223): an operator
// secret reaches the script-tool child through env passthrough — the
// config file carries no value — and the sidecar marks itself
// non-dumpable, so a same-account reader cannot pull the secret back
// out of /proc/<pid>/environ. Verified on a uid-1000 host with
// ptrace_scope=0 and no sudo.
func TestEndToEnd_SidecarSingleAccountSecretCustody(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("single-account custody is only meaningful without root")
	}
	binary := buildGagentBinary(t)
	workspace := filepath.Join(t.TempDir(), "workspace")

	// The config declares the passthrough by name only — the value
	// lives solely in the sidecar's environment.
	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.json")
	require.NoError(t, os.WriteFile(schema, []byte(`{"type":"object"}`), 0o600))
	configPath := filepath.Join(dir, "sidecar.yaml")
	contents := "tools:\n" +
		"  - name: print-token\n" +
		"    description: Print the custody test token.\n" +
		"    input_schema: " + schema + "\n" +
		"    output_schema: " + schema + "\n" +
		"    exec:\n" +
		`      command: ["sh", "-c", "printf %s \"$CUSTODY_TEST_TOKEN\""]
` +
		"      env: [CUSTODY_TEST_TOKEN]\n"
	require.NoError(t, os.WriteFile(configPath, []byte(contents), 0o600))

	listen, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listen.Addr().String()
	require.NoError(t, listen.Close())

	logPath := filepath.Join(t.TempDir(), "sidecar.log")
	logFile, err := os.Create(logPath)
	require.NoError(t, err)
	t.Cleanup(func() { logFile.Close() })

	cmd := exec.Command(binary, "sidecar",
		"--config", configPath, "--addr", addr, "--workspace", workspace,
		"--exec-uid", fmt.Sprint(os.Geteuid()))
	cmd.Env = append(os.Environ(), "CUSTODY_TEST_TOKEN=s3cret-custody")
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
	var descs []tool.Descriptor
	require.Eventually(t, func() bool {
		descs, err = tool.Discover(context.Background(), baseURL)
		return err == nil
	}, 10*time.Second, 20*time.Millisecond, "the sidecar must serve the contract\nlogs:\n%s", readLogs(logPath))
	require.Len(t, descs, 3)
	byName := make(map[string]tool.Descriptor, len(descs))
	for _, d := range descs {
		byName[d.Name] = d
	}

	// The passthrough secret reaches the tool child's env.
	printToken := tool.NewRemoteTool(byName["print-token"], baseURL)
	out, err := tool.Invoke(context.Background(), printToken, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "s3cret-custody", string(out))
	raw, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "s3cret",
		"the config file carries the passthrough by name only, never the value")

	// The sidecar is non-dumpable: a same-account reader cannot pull
	// its exec-time environment (the secret) out of /proc.
	_, err = os.ReadFile(fmt.Sprintf("/proc/%d/environ", cmd.Process.Pid))
	require.Error(t, err, "a non-dumpable sidecar's environ must be unreadable to same-account processes")
	assert.True(t, os.IsPermission(err),
		"the read fails with EACCES, got: %v", err)

	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	select {
	case err := <-exited:
		require.NoError(t, err, "sidecar must exit cleanly on SIGTERM")
	case <-time.After(15 * time.Second):
		t.Fatal("sidecar did not exit on SIGTERM")
	}
}

// readLogs returns a log file's contents for test failure messages.
func readLogs(path string) string {
	logs, _ := os.ReadFile(path)
	return string(logs)
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
