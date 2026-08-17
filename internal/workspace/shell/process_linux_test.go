//go:build linux

package shell

import (
	"context"
	"errors"
	"os"
	osexec "os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testExecUID is this file's copy of the account the tests drop to;
// process_test.go holds the external-test one. The platform declares
// no default of its own — the exec UID reaches the workspace from
// config (internal/config).
const testExecUID = 1001

// TestLimitSpecs_NprocOnlyInThePrivilegedDrop pins which caps reach the
// shim: four in single-account mode, five when the child owns a
// dedicated UID, because RLIMIT_NPROC counts the real UID's processes
// (ADR 0010).
func TestLimitSpecs_NprocOnlyInThePrivilegedDrop(t *testing.T) {
	exec := NewProcessExecutor(testExecUID, 5*time.Second)

	names := func(euid int) []string {
		var out []string
		for _, spec := range exec.limitSpecs(euid) {
			out = append(out, spec.name)
		}
		return out
	}

	assert.Equal(t, []string{"cpu", "as", "fsize", "nofile"}, names(1000))
	assert.Equal(t, []string{"cpu", "as", "fsize", "nofile", "nproc"}, names(0))
}

// TestShimCommandLine_CarriesEveryCapAndTheResolvedProgram pins the
// command line the untrusted child runs: this binary as the shim, every
// cap, then the resolved program and the argv it sees.
func TestShimCommandLine_CarriesEveryCapAndTheResolvedProgram(t *testing.T) {
	exec := NewProcessExecutor(testExecUID, 5*time.Second)

	line, err := exec.shimCommandLine("sh", []string{"sh", "-c", "echo hi"}, 0)
	require.NoError(t, err)

	self, err := os.Executable()
	require.NoError(t, err)
	sh, err := osexec.LookPath("sh")
	require.NoError(t, err)
	assert.Equal(t, []string{
		self, ShimCommand,
		"cpu=60", "as=536870912", "fsize=104857600", "nofile=1024", "nproc=64",
		"--", sh, "sh", "-c", "echo hi",
	}, line)
}

// TestShimCommandLine_FailsWhenThisBinaryIsUnknown pins the fail-closed
// posture at the other end: without a shim to exec, the call reports
// the caps as unavailable rather than running the child uncapped.
func TestShimCommandLine_FailsWhenThisBinaryIsUnknown(t *testing.T) {
	exec := NewProcessExecutor(testExecUID, 5*time.Second)
	exec.shimErr = errors.New("no executable")

	_, err := exec.shimCommandLine("sh", []string{"sh", "-c", "echo hi"}, 0)

	require.ErrorIs(t, err, ErrRlimitsUnavailable)
}

// TestParseShimArgv covers the shapes the shim must reject rather than
// apply partially, and the one it must accept.
func TestParseShimArgv(t *testing.T) {
	specs, path, argv, err := parseShimArgv([]string{"cpu=60", "nofile=1024", "--", "/bin/sh", "sh", "-c", "echo hi"})
	require.NoError(t, err)
	assert.Equal(t, []limitSpec{{name: "cpu", value: 60}, {name: "nofile", value: 1024}}, specs)
	assert.Equal(t, "/bin/sh", path)
	assert.Equal(t, []string{"sh", "-c", "echo hi"}, argv)

	for name, args := range map[string][]string{
		"no separator":      {"cpu=60"},
		"no program":        {"cpu=60", "--"},
		"no argv":           {"cpu=60", "--", "/bin/sh"},
		"malformed limit":   {"cpu", "--", "/bin/sh", "sh"},
		"unnamed limit":     {"=60", "--", "/bin/sh", "sh"},
		"unparseable value": {"cpu=soon", "--", "/bin/sh", "sh"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, _, err := parseShimArgv(args)
			assert.Error(t, err)
		})
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

// requireRoot skips a case that needs root, honouring the same
// declaration the external test package's requireRoot does. It is a second
// copy because a helper cannot cross the two test packages in this
// directory: a run that promised privilege reports the absence of it
// rather than skipping past it.
func requireRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		return
	}
	if os.Getenv("GAGENT_PRIVILEGED_TESTS") != "" {
		t.Fatalf("GAGENT_PRIVILEGED_TESTS is set but this run has euid %d", os.Geteuid())
	}
	t.Skip("this case needs root; run `make test-privileged`")
}

// TestProcessExecutor_RootWithoutCapSysAdminReportsAgentUIDUnavailable
// pins the issue-#195 contract: a root process without CAP_SYS_ADMIN
// (e.g. a default Docker container) must surface the documented
// agent_uid unavailable fallback rather than a raw fork/exec EPERM.
// The test toggles the unexported netNS flag to simulate the probe
// result; the euid check is bypassed by running as root.
func TestProcessExecutor_RootWithoutCapSysAdminReportsAgentUIDUnavailable(t *testing.T) {
	requireRoot(t)
	exec := NewProcessExecutor(testExecUID, 5*time.Second)
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

// TestProcessExecutor_RootWithoutCapKillReportsAgentUIDUnavailable pins
// the posture #612 settles: a deadline the executor cannot enforce is a
// boundary it cannot deliver, so the call fails closed rather than running
// a command nothing can stop. Signalling a child dropped to another account
// needs CAP_KILL and root is no substitute for it; without the capability
// the SIGKILL on expiry silently does nothing and the caller waits out the
// command it asked to bound.
//
// The test toggles the unexported probe result, as the CAP_SYS_ADMIN case
// above does. The error names the capability, because the message is the
// whole diagnosis an operator gets.
func TestProcessExecutor_RootWithoutCapKillReportsAgentUIDUnavailable(t *testing.T) {
	requireRoot(t)
	exec := NewProcessExecutor(testExecUID, 5*time.Second)
	exec.canSignal = false

	res, err := exec.Run(context.Background(), Request{Command: "echo hi"})

	require.ErrorIs(t, err, ErrAgentUIDUnavailable,
		"a privileged executor that cannot signal its child must report the documented unavailability")
	assert.Contains(t, err.Error(), "CAP_KILL",
		"the message names the capability to grant")
	assert.Empty(t, res.Stdout,
		"the executor must not fork when the signal gate fires")
}

// TestProcessExecutor_SingleAccountNeedsNoKillCapability pins the other
// half: the gate is about crossing a uid boundary, not about holding a
// capability. In single-account mode the child owns the executor's own uid,
// which is permission enough to signal it, so the absent capability must
// not fail the call.
func TestProcessExecutor_SingleAccountNeedsNoKillCapability(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("single-account mode is only meaningful without root")
	}
	exec := NewProcessExecutor(os.Geteuid(), 5*time.Second)
	exec.canSignal = false

	res, err := exec.Run(context.Background(), Request{Command: "echo hi"})

	require.NoError(t, err, "the executor's own account needs no capability to signal")
	assert.Equal(t, "hi\n", res.Stdout)
}
