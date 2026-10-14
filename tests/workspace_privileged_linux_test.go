//go:build linux

package tests

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/workspace"
)

// TestEndToEnd_PrivilegedWorkspaceExec exercises the shipped workspace and its
// exec-limits shim under the deployment's capabilities, with a distinct UID.
func TestEndToEnd_PrivilegedWorkspaceExec(t *testing.T) {
	if os.Getenv("SHERLOCK_PRIVILEGED_TESTS") == "" {
		t.Skip("run make test-privileged as root to exercise the binary privilege boundary")
	}
	require.Equal(t, 0, os.Geteuid(), "the privileged binary suite requires root")
	declared := os.Getenv("SHERLOCK_DEPLOYED_CAPS")
	require.NotEmpty(t, declared, "make test-privileged must declare the narrowed capability set")
	bits := map[string]uint{"chown": 0, "kill": 5, "setgid": 6, "setuid": 7, "sys_admin": 21}
	var wantCaps uint64
	for _, name := range strings.Split(declared, ",") {
		bit, ok := bits[name]
		require.True(t, ok, "unknown deployed capability %q", name)
		wantCaps |= 1 << bit
	}
	assertProcessCaps := func(pid string) {
		t.Helper()
		status, err := os.ReadFile("/proc/" + pid + "/status")
		require.NoError(t, err)
		for _, field := range []string{"CapEff:", "CapBnd:"} {
			require.Contains(t, strings.Fields(string(status)), field)
			for _, line := range strings.Split(string(status), "\n") {
				if strings.HasPrefix(line, field) {
					got, err := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, field)), 16, 64)
					require.NoError(t, err)
					require.Equal(t, wantCaps, got, "%s must equal the deployment set in pid %s", field, pid)
				}
			}
		}
	}
	assertProcessCaps("self")
	binary := os.Getenv("SHERLOCK_TEST_BINARY")
	require.NotEmpty(t, binary, "make test-privileged must build the shipped binary before narrowing")
	info, err := os.Stat(binary)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&0o111, "the shipped binary must be executable by the exec account")

	// A traversable parent separates DAC refusal from an inaccessible fixture.
	root, err := os.MkdirTemp("", "sherlock-privileged-workspace-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(root)) })
	require.NoError(t, os.Chmod(root, 0o755))
	const execUID = 61040
	workspaceDir := filepath.Join(root, "workspace")
	t.Setenv("SHERLOCK_BINARY_TEST_SECRET", "workspace-marker")
	cmd, addr, _ := startWorkspaceBinaryAs(t, binary, workspaceDir, emptyConfig(t, "workspace.yaml"), execUID)
	assertProcessCaps(strconv.Itoa(cmd.Process.Pid))
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", cmd.Process.Pid))
	require.NoError(t, err)
	require.Regexp(t, `(?m)^Uid:\s+0\s+0\s+0\s+0$`, string(status), "the actual workspace process runs as root")
	ws, closeConn := dialWorkspace(t, addr)
	defer closeConn()
	run := func(t *testing.T, req *workspace.ExecRequest) *workspace.ExecResponse {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		resp, err := ws.Exec(ctx, req)
		require.NoError(t, err, "the required Exec boundary must be available")
		return resp
	}
	ok := func(t *testing.T, command string) string {
		t.Helper()
		resp := run(t, &workspace.ExecRequest{Command: command})
		require.False(t, resp.TimedOut)
		require.Zero(t, resp.ExitCode, "%s: %s", command, resp.Stderr)
		return resp.Stdout
	}

	t.Run("child credentials and DAC", func(t *testing.T) {
		assert.Equal(t, "61040\n61040\n", ok(t, "id -u; id -g"))
		childStatus := ok(t, "cat /proc/self/status")
		assert.Regexp(t, `(?m)^CapEff:\s+0+$`, childStatus)
		private := filepath.Join(root, "owner-only")
		require.NoError(t, os.WriteFile(private, []byte("private"), 0o600))
		public := filepath.Join(root, "readable")
		require.NoError(t, os.WriteFile(public, []byte("control"), 0o644))
		assert.Equal(t, "control", ok(t, "cat "+public), "the child can traverse the fixture and read allowed content")
		denied := run(t, &workspace.ExecRequest{Command: "cat " + private})
		assert.NotZero(t, denied.ExitCode)
		assert.Empty(t, denied.Stdout)
		assert.Contains(t, denied.Stderr, "Permission denied")
	})
	t.Run("contract and Exec share writable files", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := ws.Write(ctx, &workspace.WriteRequest{Path: "sub/shared", Content: []byte("contract")})
		require.NoError(t, err)
		for _, path := range []string{workspaceDir, filepath.Join(workspaceDir, "sub"), filepath.Join(workspaceDir, "sub/shared")} {
			info, err := os.Stat(path)
			require.NoError(t, err)
			stat, valid := info.Sys().(*syscall.Stat_t)
			require.True(t, valid)
			assert.EqualValues(t, execUID, stat.Uid, "ownership of %s", path)
		}
		assert.Equal(t, workspaceDir+"\n", ok(t, "pwd"))
		ok(t, "printf '+exec' >> sub/shared; printf 'child' > child; chmod 600 child")
		read, err := ws.Read(ctx, &workspace.ReadRequest{Path: "sub/shared"})
		require.NoError(t, err)
		assert.Equal(t, "contract+exec", string(read.Content))
		_, err = ws.Write(ctx, &workspace.WriteRequest{Path: "child", Content: []byte("replacement")})
		require.NoError(t, err, "root without DAC_OVERRIDE replaces a child-owned owner-only file")
		assert.Equal(t, "replacement", ok(t, "cat child"))
	})
	t.Run("workspace environment is scrubbed", func(t *testing.T) {
		environ, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", cmd.Process.Pid))
		require.NoError(t, err)
		require.Contains(t, string(environ), "SHERLOCK_BINARY_TEST_SECRET=workspace-marker\x00", "the workspace actually inherited the marker")
		assert.Equal(t, "unset\n", ok(t, "printf '%s\\n' \"${SHERLOCK_BINARY_TEST_SECRET-unset}\""))
		control := run(t, &workspace.ExecRequest{Command: "printf '%s' \"$SHERLOCK_BINARY_TEST_SECRET\"", Env: []string{"SHERLOCK_BINARY_TEST_SECRET=explicit"}})
		require.Zero(t, control.ExitCode, control.Stderr)
		assert.Equal(t, "explicit", control.Stdout, "request environment is still delivered")
	})
	t.Run("NoNetwork creates fresh empty namespaces", func(t *testing.T) {
		// A default Exec child shares the workspace's namespace and reads its
		// own link; the workspace's link refuses the narrowed test (EACCES).
		parentNS := strings.TrimSpace(ok(t, "readlink /proc/self/ns/net"))
		require.True(t, strings.HasPrefix(parentNS, "net:["), parentNS)
		var previous string
		for range 2 {
			resp := run(t, &workspace.ExecRequest{Command: "readlink /proc/self/ns/net; cat /proc/net/dev; tail -n +2 /proc/net/route | wc -l", NoNetwork: true})
			require.Zero(t, resp.ExitCode, resp.Stderr)
			require.False(t, resp.TimedOut)
			lines := strings.Split(strings.TrimSpace(resp.Stdout), "\n")
			require.Len(t, lines, 5, "only lo and the route entry count exist in the fresh namespace")
			assert.NotEqual(t, parentNS, lines[0], "NoNetwork must leave the workspace namespace")
			assert.NotEqual(t, previous, lines[0], "each call creates a new namespace")
			assert.Equal(t, "lo", strings.TrimSpace(strings.SplitN(lines[3], ":", 2)[0]))
			// A kernel without a main table yet prints no header either, so the
			// count skips the first line rather than expecting it.
			assert.Equal(t, "0", strings.TrimSpace(lines[4]), "the routing table is empty")
			previous = lines[0]
		}
	})
	t.Run("untrusted children carry resource caps", func(t *testing.T) {
		limits := ok(t, "cat /proc/self/limits")
		for _, pattern := range []string{
			`Max cpu time\s+60\s+60\s+seconds`,
			`Max address space\s+2147483648\s+2147483648\s+bytes`,
			`Max file size\s+104857600\s+104857600\s+bytes`,
			`Max open files\s+1024\s+1024\s+files`,
			`Max processes\s+64\s+64\s+processes`,
		} {
			assert.Regexp(t, pattern, limits)
		}
		control := run(t, &workspace.ExecRequest{Command: "cat /proc/self/limits", Trusted: true})
		require.Zero(t, control.ExitCode, control.Stderr)
		assert.Regexp(t, `Max cpu time\s+unlimited\s+unlimited`, control.Stdout, "the trusted control proves the CPU cap was applied by the shim")
	})
	t.Run("deadline kills the dropped process group", func(t *testing.T) {
		start := time.Now()
		resp := run(t, &workspace.ExecRequest{Command: "sleep 5; echo survived", TimeoutNanos: int64(100 * time.Millisecond)})
		assert.True(t, resp.TimedOut)
		assert.Empty(t, resp.Stdout)
		assert.Less(t, time.Since(start), 3*time.Second, "descendants holding the output pipe must also be killed")
	})
	t.Log("exercised 6 binary Exec cases: credentials/DAC, file ownership and replacement, scrubbed environment, two fresh empty netns, five rlimits/trusted control, process-group deadline")
}
