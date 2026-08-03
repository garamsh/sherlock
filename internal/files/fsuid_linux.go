//go:build linux

package files

import (
	"fmt"
	"log/slog"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

// withFSUID runs fn with the calling thread's filesystem UID set to uid,
// so files fn creates are owned by the agent UID (ADR 0006) while the
// process keeps its real and effective UIDs. The drop applies only to a
// privileged caller (euid 0); otherwise fn runs under the current UID,
// which is the unprivileged platform's own. fsuid is per-thread, so the
// thread is locked for the duration.
func withFSUID(uid int, fn func() error) error {
	if uid <= 0 || os.Geteuid() != 0 {
		return fn()
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	prev, err := unix.SetfsuidRetUid(uid)
	if err != nil {
		return fmt.Errorf("drop fsuid to %d: %w", uid, err)
	}
	defer func() {
		// Restore before unlocking: a leaked fsuid would follow the
		// thread into unrelated goroutines.
		if _, err := unix.SetfsuidRetUid(prev); err != nil {
			slog.Error("files: fsuid restore failed", "fsuid", prev, "error", err)
		}
	}()
	return fn()
}
