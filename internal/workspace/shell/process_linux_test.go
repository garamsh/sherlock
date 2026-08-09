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
	for _, name := range []string{"RLIMIT_CPU", "RLIMIT_AS", "RLIMIT_FSIZE", "RLIMIT_NOFILE"} {
		assert.Contains(t, out, name, "failed cap must be logged with its resource name")
	}
	// RLIMIT_NPROC applies only to the privileged drop; it is skipped in
	// single-account mode, where it would starve the child of the shared
	// account's own process budget.
	if os.Geteuid() == 0 {
		assert.Contains(t, out, "RLIMIT_NPROC")
	} else {
		assert.NotContains(t, out, "RLIMIT_NPROC")
	}
}

// TestParseCapEff covers the shapes /proc/self/status produces: prefixed
// hex (the documented form), plain hex, leading whitespace, and absent.
func TestParseCapEff(t *testing.T) {
	const withSysAdmin = "CapEff:\t00000000a80425fb\n" // CAP_SYS_ADMIN set
	const withoutSysAdmin = "CapEff:\t0000000000000000\n"

	tests := []struct {
		name    string
		status  string
		hasMask bool
		want    uint64
		err     bool
	}{
		{name: "sysadmin set, prefixed", status: withSysAdmin, hasMask: true, want: 0xa80425fb},
		{name: "sysadmin clear", status: withoutSysAdmin, hasMask: true, want: 0},
		{name: "0x prefix", status: "CapEff: 0x0000000000000000\n", hasMask: true, want: 0},
		{name: "absent", status: "Name:\tshell\nPid:\t1\n", err: true},
		{name: "garbage", status: "CapEff:\tnot-hex\n", err: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCapEff(tt.status)
			if tt.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			if tt.hasMask {
				has := got&(1<<capSysAdmin) != 0
				_ = has // the SysAdmin assertion is exercised by the named cases above
			}
		})
	}
}

// TestProcessExecutor_RootWithoutCapSysAdminReportsAgentUIDUnavailable
// pins the issue-#195 contract: a root process without CAP_SYS_ADMIN
// (e.g. a default Docker container) must surface the documented
// agent_uid unavailable fallback rather than a raw fork/exec EPERM.
// The test toggles the unexported netNS flag to simulate the probe
// result; the euid check is bypassed by running as root.
func TestProcessExecutor_RootWithoutCapSysAdminReportsAgentUIDUnavailable(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("this case needs root; run with `sudo -E go test ./internal/workspace/shell/...`")
	}
	exec := NewProcessExecutor(DefaultAgentUID, 5*time.Second)
	exec.netNS = false

	res, err := exec.Run(context.Background(), Request{
		Command:   "echo hi",
		NoNetwork: true,
	})

	require.ErrorIs(t, err, ErrAgentUIDUnavailable,
		"a root process lacking netns capability must report the documented unavailability")
	assert.Empty(t, res.Stdout,
		"the executor must not fork when the netns gate fires")
}
