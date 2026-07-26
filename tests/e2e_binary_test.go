package tests

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/docker/docker/errdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// binaryEnvID is this scenario's own environment ID; the matching
// container name is gagent-env-e2e-bin.
const binaryEnvID = "e2e-bin"

// TestEndToEnd_BinaryMVPScenario drives the PLAN §5.7 scenario against the
// built gagent binary served as a subprocess — the e2e layer of
// docs/convention/testing.md: real binary, real HTTP, real Docker daemon,
// nothing mocked beyond the scripted mock model provider.
func TestEndToEnd_BinaryMVPScenario(t *testing.T) {
	cli := requireDockerClient(t)
	binary := buildGagentBinary(t)
	root, envDir, seedID := setupMVPScenario(t, cli, binaryEnvID)

	cmd, baseURL, exited := startGatewayBinary(t, binary, root)

	runMVPScenario(t, baseURL, root, binaryEnvID)

	// SIGTERM triggers the binary's graceful shutdown, which destroys the
	// environment container before exiting.
	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	select {
	case err := <-exited:
		require.NoError(t, err, "gateway must exit cleanly on SIGTERM")
	case <-time.After(15 * time.Second):
		t.Fatal("gateway did not exit on SIGTERM")
	}
	_, err := cli.ContainerInspect(context.Background(), "gagent-env-"+binaryEnvID)
	assert.True(t, errdefs.IsNotFound(err),
		"environment container must be removed on shutdown, got: %v", err)

	assertMVPRecallAndWriteback(t, envDir, seedID)
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

	listen, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listen.Addr().String()
	require.NoError(t, listen.Close())

	aux := t.TempDir()
	configPath := filepath.Join(aux, "gagent.yaml")
	require.NoError(t, os.WriteFile(configPath, nil, 0o600))
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
