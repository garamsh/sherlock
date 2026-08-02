//go:build linux

package shell_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/shell"
)

// TestNetnsProbeHelper is the child process the netns test execs: it
// dials the listener address handed over via the environment and exits
// 0 on success, 1 on failure. The env gate keeps it inert under `go test`.
func TestNetnsProbeHelper(t *testing.T) {
	addr := os.Getenv("GAGENT_NETNS_PROBE_ADDR")
	if addr == "" {
		return
	}
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		os.Exit(1)
	}
	conn.Close()
	os.Exit(0)
}

// probeBinary copies the test binary into a world-accessible path so the
// child — running as the agent UID — can exec it as the probe.
func probeBinary(t *testing.T) string {
	t.Helper()
	src, err := os.Executable()
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o755))
	dst := filepath.Join(dir, "probe")
	data, err := os.ReadFile(src)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(dst, data, 0o755))
	return dst
}

// TestProcessExecutor_NoNetworkBlocksLoopback runs the same probe child
// with and without NoNetwork: in a fresh, empty network namespace the
// dial to the host loopback listener fails; without the namespace it
// succeeds (ADR 0014 — shell_exec children have no network at all).
func TestProcessExecutor_NoNetworkBlocksLoopback(t *testing.T) {
	requireRoot(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	req := shell.Request{
		Command: fmt.Sprintf("%s -test.run=TestNetnsProbeHelper", probeBinary(t)),
		Env:     []string{"GAGENT_NETNS_PROBE_ADDR=" + ln.Addr().String()},
	}
	exec := shell.NewProcessExecutor(shell.DefaultAgentUID, 5*time.Second)

	res, err := exec.Run(context.Background(), shell.Request{
		Command:   req.Command,
		Env:       req.Env,
		NoNetwork: true,
	})
	require.NoError(t, err)
	assert.NotZero(t, res.ExitCode,
		"the child in a fresh netns must not reach the host loopback")

	res, err = exec.Run(context.Background(), req)
	require.NoError(t, err)
	assert.Zero(t, res.ExitCode,
		"the same child without the namespace reaches the loopback")
}

// TestProcessExecutor_LoopbackDownInNamespace observes the namespace
// directly: a fresh netns holds only the loopback interface, and it is
// down (flags 0x8 — LOOPBACK without UP/RUNNING).
func TestProcessExecutor_LoopbackDownInNamespace(t *testing.T) {
	requireRoot(t)
	exec := shell.NewProcessExecutor(shell.DefaultAgentUID, 5*time.Second)

	res, err := exec.Run(context.Background(), shell.Request{
		Command:   "ls /sys/class/net && cat /sys/class/net/lo/flags",
		NoNetwork: true,
	})

	require.NoError(t, err)
	assert.Equal(t, "lo\n0x8\n", res.Stdout,
		"the namespace is empty but for a down loopback")
}
