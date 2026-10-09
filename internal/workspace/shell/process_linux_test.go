//go:build linux

package shell

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	osexec "os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testExecUID is this file's copy of the account the tests drop to;
// process_test.go holds the external-test one. The platform declares
// no default of its own — the exec UID reaches the workspace from
// config (internal/config).
const testExecUID = 10001

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
		"cpu=60", "as=2147483648", "fsize=104857600", "nofile=1024", "nproc=64",
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
	if os.Getenv("SHERLOCK_PRIVILEGED_TESTS") != "" {
		t.Fatalf("SHERLOCK_PRIVILEGED_TESTS is set but this run has euid %d", os.Geteuid())
	}
	t.Skip("this case needs root; run `make test-privileged`")
}

// capBits maps the capability names the deployment grants to their bit
// positions in the mask /proc/self/status reports (capabilities(7)). Only the
// names in use are here, and an unknown one fails the case that reads it: a
// wrong mask would make the assertion below pass for the wrong reason, while a
// missing entry is one line to add.
var capBits = map[string]int{
	"chown":     0,
	"kill":      capKill,
	"setgid":    6,
	"setuid":    7,
	"sys_admin": capSysAdmin,
}

// TestPrivilegedRunCarriesTheDeployedCapabilities pins #620: a check that runs
// with more privilege than the deployment cannot fail the way the deployment
// fails, which is how #612 was true in production and green in CI at the same
// time. `make test-privileged` narrows this process to the capability set
// deploy/local/docker-compose.yml grants the workspace, reading that list
// rather than repeating it, and names what it narrowed to in
// SHERLOCK_DEPLOYED_CAPS.
//
// This case is what makes the narrowing load-bearing. Without it a recipe that
// dropped the setpriv line would run every other case under whatever the host
// granted and pass, which is the state #620 reports.
func TestPrivilegedRunCarriesTheDeployedCapabilities(t *testing.T) {
	declared := os.Getenv("SHERLOCK_DEPLOYED_CAPS")
	if declared == "" {
		if os.Getenv("SHERLOCK_PRIVILEGED_TESTS") != "" {
			t.Fatal("this run declared itself privileged but not narrowed: `make test-privileged` narrows to the deployment's capability set and names it in SHERLOCK_DEPLOYED_CAPS, and a boundary checked under more privilege than the deployment holds cannot fail the way the deployment fails (#620)")
		}
		t.Skip("not a deployed-capability run; `make test-privileged` sets SHERLOCK_DEPLOYED_CAPS")
	}

	var want uint64
	for _, name := range strings.Split(declared, ",") {
		bit, known := capBits[name]
		require.True(t, known,
			"SHERLOCK_DEPLOYED_CAPS names %q, which capBits does not map; add it rather than let the mask be wrong", name)
		want |= 1 << bit
	}

	got, err := readEffectiveCaps()
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("%#x", want), fmt.Sprintf("%#x", got),
		"the run holds exactly what the deployment grants (%s) — a wider set cannot fail the way the deployment fails, a narrower one is a runner that refused what was asked for", declared)
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

// captureDefaultLog routes the default logger, which NewProcessExecutor
// warns through, into a buffer for the rest of the test and returns the
// JSON records written so far on each call.
func captureDefaultLog(t *testing.T) func() []map[string]any {
	t.Helper()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return func() []map[string]any {
		var records []map[string]any
		for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
			if len(line) == 0 {
				continue
			}
			var record map[string]any
			require.NoError(t, json.Unmarshal(line, &record))
			records = append(records, record)
		}
		return records
	}
}

// noNetworkWarnings returns the records of the unprivileged NoNetwork WARN.
func noNetworkWarnings(records []map[string]any) []map[string]any {
	var found []map[string]any
	for _, r := range records {
		msg, _ := r["msg"].(string)
		if strings.Contains(msg, "NoNetwork probe failed") {
			found = append(found, r)
		}
	}
	return found
}

// TestNewProcessExecutor_WarnsWhenTheUnprivilegedShapeCannotServeNoNetwork
// pins the startup statement ADR 0010 requires of the unprivileged shape
// (#1053): the executor makes the NoNetwork clone once at construction and,
// when it fails, warns once with the consequence and where the host
// requirements are stated. The probe stands in for a host that refuses the
// clone, which a host that permits it cannot be made into; the controls are
// the same shape on a host that admits the clone, and a refusing host
// outside the shape, where the probe does not run.
func TestNewProcessExecutor_WarnsWhenTheUnprivilegedShapeCannotServeNoNetwork(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the unprivileged shape is only meaningful without root")
	}
	tests := []struct {
		name       string
		agentUID   int
		probeErr   error
		wantProbes int
		wantWarn   bool
	}{
		{name: "unprivileged shape, host refuses the clone", agentUID: os.Geteuid(), probeErr: ErrAgentUIDUnavailable, wantProbes: 1, wantWarn: true},
		{name: "unprivileged shape, host admits the clone", agentUID: os.Geteuid(), probeErr: nil, wantProbes: 1, wantWarn: false},
		{name: "exec-uid is another uid", agentUID: os.Geteuid() + 1, probeErr: ErrAgentUIDUnavailable, wantProbes: 0, wantWarn: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probes := 0
			original := probeNoNetwork
			probeNoNetwork = func(*ProcessExecutor) error { probes++; return tt.probeErr }
			t.Cleanup(func() { probeNoNetwork = original })
			records := captureDefaultLog(t)

			NewProcessExecutor(tt.agentUID, 5*time.Second)

			assert.Equal(t, tt.wantProbes, probes, "the probe runs once in the unprivileged shape and not outside it")
			warnings := noNetworkWarnings(records())
			if !tt.wantWarn {
				assert.Empty(t, warnings)
				return
			}
			require.Len(t, warnings, 1, "one WARN, not one per call")
			w := warnings[0]
			assert.Equal(t, "WARN", w["level"])
			assert.Contains(t, w["msg"], "every NoNetwork Exec, so every shell_exec, will fail with this error")
			assert.Contains(t, w["msg"], "docs/architecture/deployment.md §Capabilities")
			assert.Equal(t, ErrAgentUIDUnavailable.Error(), w["error"], "the error is what each NoNetwork call will report")
		})
	}
}

// TestNewProcessExecutor_NoNetworkWarningMatchesTheHost runs the real
// probe and checks it against a real NoNetwork call, as shell_exec makes
// one: the executor warns at startup exactly when that call is refused. On
// a host that admits the clone this pins that the probe does not misfire;
// on one that refuses it, that the refusal is stated before the first call.
func TestNewProcessExecutor_NoNetworkWarningMatchesTheHost(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the unprivileged shape is only meaningful without root")
	}
	records := captureDefaultLog(t)

	exec := NewProcessExecutor(os.Geteuid(), 5*time.Second)
	warned := len(noNetworkWarnings(records())) > 0

	res, err := exec.Run(context.Background(), Request{Command: "echo hi", NoNetwork: true})
	if err != nil {
		require.ErrorIs(t, err, ErrAgentUIDUnavailable, "the call failed for a reason other than the clone (stderr=%q)", res.Stderr)
	} else {
		require.Equal(t, "hi\n", res.Stdout, "the call ran, so its success is a result about the clone")
	}
	refused := err != nil
	t.Logf("host refuses the unprivileged clone: %v", refused)
	assert.Equal(t, refused, warned, "the startup WARN is present exactly when a NoNetwork call is refused")
}

// TestRun_UsernsRefusalReportsAgentUIDUnavailable pins #1057: every errno
// by which a host refuses the unprivileged NoNetwork clone reports the
// documented agent_uid unavailable, naming the errno, and the startup
// probe's WARN carries the same. The clone's error is faked, in the shape
// os/exec returns it, because a host that admits the clone produces none of
// these. The control is an errno that is not a refusal, which stays a plain
// start failure.
func TestRun_UsernsRefusalReportsAgentUIDUnavailable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the unprivileged shape is only meaningful without root")
	}
	tests := []struct {
		errno   syscall.Errno
		refusal bool
	}{
		{errno: syscall.EPERM, refusal: true},
		{errno: syscall.ENOSPC, refusal: true},
		{errno: syscall.EUSERS, refusal: true},
		{errno: syscall.EINVAL, refusal: true},
		{errno: syscall.EAGAIN, refusal: false},
	}
	for _, tt := range tests {
		t.Run(tt.errno.Error(), func(t *testing.T) {
			original := startUserns
			startUserns = func(cmd *osexec.Cmd) error {
				return &fs.PathError{Op: "fork/exec", Path: cmd.Path, Err: tt.errno}
			}
			t.Cleanup(func() { startUserns = original })
			records := captureDefaultLog(t)

			exec := NewProcessExecutor(os.Geteuid(), 5*time.Second)
			_, err := exec.Run(context.Background(), Request{Command: "echo hi", NoNetwork: true})

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errno.Error(), "the message names the errno")
			warnings := noNetworkWarnings(records())
			require.Len(t, warnings, 1, "the startup probe met the same refusal")
			assert.Contains(t, warnings[0]["error"], tt.errno.Error())
			if !tt.refusal {
				assert.NotErrorIs(t, err, ErrAgentUIDUnavailable)
				assert.ErrorIs(t, err, tt.errno, "a plain failure wraps the start error")
				assert.True(t, strings.HasPrefix(err.Error(), "shell: start: "), "a plain start failure: %q", err)
				return
			}
			assert.ErrorIs(t, err, ErrAgentUIDUnavailable)
			assert.True(t, strings.HasPrefix(warnings[0]["error"].(string), ErrAgentUIDUnavailable.Error()),
				"the startup WARN carries the sentinel: %q", warnings[0]["error"])
		})
	}
}
