//go:build linux

package shell

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"golang.org/x/sys/unix"
)

// TestApplyRLimits_LogsFailures verifies that a prlimit failure is
// surfaced as a WARN log naming the resource, not silently discarded
// (runtime-safety.md §Anti-patterns). An invalid PID fails every cap.
func TestApplyRLimits_LogsFailures(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	exec := NewProcessExecutor(DefaultAgentUID, 5*time.Second)
	exec.applyRLimits(-1)

	out := buf.String()
	for _, name := range []string{"RLIMIT_CPU", "RLIMIT_AS", "RLIMIT_FSIZE", "RLIMIT_NOFILE", "RLIMIT_NPROC"} {
		assert.Contains(t, out, name, "failed cap must be logged with its resource name")
	}
}

// withProbeNetns swaps probeNetns for the duration of t and restores the
// original on cleanup. Tests use it to simulate the "root without
// CAP_SYS_ADMIN" container state (issue #195) without dropping caps in
// the host test runner.
func withProbeNetns(t *testing.T, fn func() bool) {
	t.Helper()
	prev := probeNetns
	probeNetns = fn
	t.Cleanup(func() { probeNetns = prev })
}

// TestProcessExecutor_NetnsCapMissingReportsUnavailable is the regression
// for issue #195: a root platform without CAP_SYS_ADMIN must surface
// the documented agent_uid unavailable contract instead of a raw
// fork/exec EPERM. With the probe forced to "false" the request asks
// for a fresh network namespace; the executor must short-circuit before
// fork and emit a WARN naming the missing capability.
func TestProcessExecutor_NetnsCapMissingReportsUnavailable(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("the cap-missing path is only meaningful when the executor can otherwise drop privileges")
	}
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	withProbeNetns(t, func() bool { return false })
	exec := NewProcessExecutor(DefaultAgentUID, 5*time.Second)

	_, err := exec.Run(context.Background(), Request{
		Command:   "echo hi",
		NoNetwork: true,
	})

	require.ErrorIs(t, err, ErrAgentUIDUnavailable,
		"the model must see the documented unavailable contract")
	assert.Contains(t, buf.String(), "netns capability missing",
		"the degradation is logged so operators can diagnose the missing capability")
	assert.Contains(t, buf.String(), "CAP_SYS_ADMIN",
		"the log names the missing capability")
}

// TestProcessExecutor_NetnsCapMissingAllowsNonNetnsRequest verifies that
// the cap probe only blocks the netns path: a script-tool request
// (NoNetwork=false) still runs through the executor when CAP_SYS_ADMIN
// is missing, because script tools keep network access as the
// operator-sanctioned egress channel (ADR 0014).
func TestProcessExecutor_NetnsCapMissingAllowsNonNetnsRequest(t *testing.T) {
	requireRoot(t)
	withProbeNetns(t, func() bool { return false })
	exec := NewProcessExecutor(DefaultAgentUID, 5*time.Second)

	res, err := exec.Run(context.Background(), Request{Command: "echo hi"})

	require.NoError(t, err)
	assert.Equal(t, "hi\n", res.Stdout,
		"a request that does not need a fresh netns must run without CAP_SYS_ADMIN")
}

// TestProcessExecutor_NetnsCapPresentKeepsPath exercises the happy
// path: with the probe reporting the capability is present, the
// executor proceeds as before and drops into the namespace.
func TestProcessExecutor_NetnsCapPresentKeepsPath(t *testing.T) {
	requireRoot(t)
	withProbeNetns(t, func() bool { return true })
	exec := NewProcessExecutor(DefaultAgentUID, 5*time.Second)

	res, err := exec.Run(context.Background(), Request{Command: "echo hi"})

	require.NoError(t, err)
	assert.Equal(t, "hi\n", res.Stdout)
}

// TestDefaultProbeNetns_RunsWithoutPanic is the smoke test for the
// production probe: it parses /proc/self/status CapEff on the test
// process and returns a bool. It does not assert a specific value
// (the host's cap set is out of the test's control); it only guards
// against panics on malformed /proc layouts.
func TestDefaultProbeNetns_RunsWithoutPanic(t *testing.T) {
	v := defaultProbeNetns()
	assert.IsType(t, true, v)
}

// TestParseCapEff covers the parsing half of defaultProbeNetns in
// isolation: given a synthetic /proc/self/status body, the parser
// returns the expected "capability present" / "capability absent"
// answer. A wrong "yes" here would mean the default probe is silently
// reporting the executor as available when it isn't.
func TestParseCapEff(t *testing.T) {
	withCapSysAdmin := uint64(1) << unix.CAP_SYS_ADMIN

	tests := []struct {
		name   string
		status string
		want   bool
	}{
		{
			name:   "cap present alone in the mask",
			status: "Name:\tgagent\nCapEff:\t" + uintToHex(withCapSysAdmin) + "\n",
			want:   true,
		},
		{
			name:   "cap absent (empty mask)",
			status: "CapEff:\t0000000000000000\n",
			want:   false,
		},
		{
			name:   "cap absent (other caps set)",
			status: "CapEff:\t" + uintToHex(uint64(1)<<unix.CAP_NET_ADMIN) + "\n",
			want:   false,
		},
		{
			name:   "cap present alongside other caps",
			status: "CapEff:\t" + uintToHex(withCapSysAdmin|uint64(1)<<unix.CAP_NET_ADMIN) + "\n",
			want:   true,
		},
		{
			name:   "CapEff line missing defaults to present",
			status: "Name:\tgagent\n",
			want:   true,
		},
		{
			name:   "CapEff malformed defaults to present",
			status: "CapEff:\tnot-a-number\n",
			want:   true,
		},
		{
			name:   "CapEff with no value defaults to present",
			status: "CapEff:\n",
			want:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCapEff(tt.status)
			assert.Equal(t, tt.want, got)
		})
	}
}

// uintToHex formats a uint64 as the 16-digit lowercase hex string
// /proc/self/status uses for CapEff.
func uintToHex(v uint64) string {
	const hexDigits = "0123456789abcdef"
	out := make([]byte, 16)
	for i := 15; i >= 0; i-- {
		out[i] = hexDigits[v&0xf]
		v >>= 4
	}
	return string(out)
}

// requireRoot skips the test when the current effective user cannot drop
// to the agent UID. The remaining branches still run; we just cannot
// exercise fork+exec without root. This duplicates the helper in
// process_test.go because the two files live in different packages
// (shell vs shell_test).
func requireRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("setuid path requires root; run with `sudo -E go test ./internal/shell/...` to exercise fork+exec")
	}
}
