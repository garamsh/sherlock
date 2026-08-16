//go:build linux

package shell_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/workspace/shell"
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

// The probe children in this file report one of two documented exit
// statuses: they connected, or they ran and could not connect. Any other
// status means the probe never ran — python3 missing (127), a shell
// error (2), a kill (above 128) — and a run that never reached the
// probe carries no evidence about the network either way.
const (
	probeConnected = 0
	probeRefused   = 1
)

// runProbe runs req and separates the two failures a bare error cannot:
// a host that cannot create the isolation namespace at all, which is an
// environment this test cannot run in, and any other error, which means
// the child never started. Neither is evidence about network isolation,
// so neither may report as one. The executor's own fail-closed contract
// is pinned in process_test.go and process_linux_test.go, not here.
func runProbe(t *testing.T, executor shell.Executor, req shell.Request) shell.Result {
	t.Helper()
	res, err := executor.Run(context.Background(), req)
	if err == nil {
		return res
	}
	if errors.Is(err, shell.ErrAgentUIDUnavailable) {
		t.Skipf("this host cannot create the isolation namespace: %v", err)
	}
	what := "the child could not be started"
	if req.NoNetwork {
		what = "the namespace could not be created"
	}
	t.Fatalf("%s, so this run says nothing about network isolation: %v (timed_out=%v, stderr=%q)",
		what, err, res.TimedOut, res.Stderr)
	return shell.Result{}
}

// requireProbeRan fails when the child's exit status is neither of the
// probe's two documented ones, naming the child's own output. Without
// it an environment failure reads as an isolation result: a probe that
// never ran exits non-zero, which is exactly what a working namespace
// looks like. The status observed in practice is 2 — the shim child's
// Go runtime aborting at pthread_create under process-creation
// pressure, which stderr names and no exit code can (#522).
func requireProbeRan(t *testing.T, res shell.Result) {
	t.Helper()
	if res.ExitCode != probeConnected && res.ExitCode != probeRefused {
		t.Fatalf("the probe child never ran, so this run says nothing about network isolation: exit=%d timed_out=%v stdout=%q stderr=%q",
			res.ExitCode, res.TimedOut, res.Stdout, res.Stderr)
	}
}

// TestProcessExecutor_NoNetworkBlocksLoopback runs the same probe child
// with and without NoNetwork: in a fresh, empty network namespace the
// dial to the host loopback listener fails; without the namespace it
// succeeds (ADR 0010 — shell_exec children have no network at all).
// Needs a root process that can unshare a network namespace; it skips
// when the host refuses one.
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
	executor := shell.NewProcessExecutor(shell.DefaultAgentUID, 5*time.Second)

	res := runProbe(t, executor, shell.Request{
		Command:   req.Command,
		Env:       req.Env,
		NoNetwork: true,
	})
	requireProbeRan(t, res)
	assert.Equal(t, probeRefused, res.ExitCode,
		"the child in a fresh netns must not reach the host loopback")

	res = runProbe(t, executor, req)
	requireProbeRan(t, res)
	assert.Equal(t, probeConnected, res.ExitCode,
		"the same child without the namespace reaches the loopback")
}

// TestProcessExecutor_LoopbackDownInNamespace observes the namespace
// directly: a fresh netns holds only the loopback interface, and it is
// down (flags 0x8 — LOOPBACK without UP/RUNNING). Needs a root process
// that can unshare a network namespace; it skips when the host refuses
// one.
func TestProcessExecutor_LoopbackDownInNamespace(t *testing.T) {
	requireRoot(t)
	executor := shell.NewProcessExecutor(shell.DefaultAgentUID, 5*time.Second)

	res := runProbe(t, executor, shell.Request{
		Command:   "ls /sys/class/net && cat /sys/class/net/lo/flags",
		NoNetwork: true,
	})

	assert.Equal(t, "lo\n0x8\n", res.Stdout,
		"the namespace is empty but for a down loopback (exit=%d, stderr=%q)",
		res.ExitCode, res.Stderr)
}

// pythonProbe returns a python3 one-liner connecting to addr and exiting
// 0 on success, 1 on failure. The single-account tests probe with python
// rather than the Go test binary: the executor's RLIMIT_AS cap is sized
// for shell commands, not a Go runtime.
func pythonProbe(addr string) string {
	host, port, _ := net.SplitHostPort(addr)
	return fmt.Sprintf(
		`python3 -c "import socket,sys; s=socket.socket(); sys.exit(0 if s.connect_ex(('%s',%s))==0 else 1)"`,
		host, port)
}

// requirePython3 skips when the child cannot find python3 on the
// scrubbed PATH.
func requirePython3(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH; the single-account netns probe needs it")
	}
}

// TestProcessExecutor_SingleAccountNoNetworkBlocksLoopback pins the
// unprivileged isolation path (ADR 0010): in single-account mode the
// shell child is network-isolated through a user+network namespace —
// no privileges required — while a child without NoNetwork keeps the
// host network (the script-tool contract). Needs a host that permits
// unprivileged user namespaces and a python3 the child can exec; it
// skips when either is missing.
func TestProcessExecutor_SingleAccountNoNetworkBlocksLoopback(t *testing.T) {
	requireUnprivileged(t)
	requirePython3(t)
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

	executor := shell.NewProcessExecutor(os.Geteuid(), 5*time.Second)

	res := runProbe(t, executor, shell.Request{
		Command:   pythonProbe(ln.Addr().String()),
		NoNetwork: true,
	})
	requireProbeRan(t, res)
	assert.Equal(t, probeRefused, res.ExitCode,
		"the child in the userns-owned netns must not reach the host loopback")

	res = runProbe(t, executor, shell.Request{Command: pythonProbe(ln.Addr().String())})
	requireProbeRan(t, res)
	assert.Equal(t, probeConnected, res.ExitCode,
		"the same child without the namespace keeps network access (the script-tool contract)")
}

// TestProcessExecutor_SingleAccountLoopbackDownInNamespace observes the
// unprivileged namespace directly: it holds only the loopback
// interface. (socket.if_nameindex is the check — /sys/class/net is not
// netns-filtered from an unprivileged userns.) Needs a host that
// permits unprivileged user namespaces and a python3 the child can
// exec; it skips when either is missing.
func TestProcessExecutor_SingleAccountLoopbackDownInNamespace(t *testing.T) {
	requireUnprivileged(t)
	requirePython3(t)
	executor := shell.NewProcessExecutor(os.Geteuid(), 5*time.Second)

	res := runProbe(t, executor, shell.Request{
		Command:   `python3 -c "import socket; print(socket.if_nameindex())"`,
		NoNetwork: true,
	})

	assert.Equal(t, "[(1, 'lo')]\n", res.Stdout,
		"the namespace is empty but for the loopback interface (exit=%d, stderr=%q)",
		res.ExitCode, res.Stderr)
}
