//go:build linux

package shell

import (
	"fmt"
	"os/exec"
	"sync"

	"golang.org/x/sys/unix"
)

// ProtectEnviron marks the calling process non-dumpable, so same-account
// processes cannot read its /proc/<pid>/environ or attach to it
// (single-account secret custody, ADR 0003): /proc/pid/environ shows the
// exec-time environment regardless of any later os.Unsetenv, so
// dumpability is the mechanism — a secret the sidecar carries in its
// environment never reaches a same-account shell_exec child through
// /proc. The executor compensates for the one side effect: a
// non-dumpable process cannot write the uid/gid maps a user-namespace
// child needs, so the userns spawn lifts the flag for the fork (see
// startUsernsChild).
func ProtectEnviron() error {
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return fmt.Errorf("prctl(PR_SET_DUMPABLE, 0): %w", err)
	}
	return nil
}

// dumpableMu serializes the dumpable lift around user-namespace spawns:
// without it, two concurrent spawns could restore a stale flag value.
var dumpableMu sync.Mutex

// startUsernsChild starts cmd, which carries CLONE_NEWUSER with uid/gid
// mappings. Those map writes are refused (EPERM) while the process is
// non-dumpable, so when custody has cleared the flag the spawn lifts it
// for the fork — the child inherits the lifted flag, which is all its
// pre-exec writes need — and restores it immediately after. The brief
// lift widens the documented residual environ exposure to the fork
// window; it does not change behavior for a normally dumpable process.
func startUsernsChild(cmd *exec.Cmd) error {
	dumpableMu.Lock()
	defer dumpableMu.Unlock()
	dumpable, err := unix.PrctlRetInt(unix.PR_GET_DUMPABLE, 0, 0, 0, 0)
	if err == nil && dumpable == 0 {
		if err := unix.Prctl(unix.PR_SET_DUMPABLE, 1, 0, 0, 0); err != nil {
			return fmt.Errorf("prctl(PR_SET_DUMPABLE, 1): %w", err)
		}
		defer unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0)
	}
	return cmd.Start()
}
