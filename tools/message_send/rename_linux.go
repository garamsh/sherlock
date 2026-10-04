package main

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// renameNoReplace renames oldpath to newpath, failing when newpath
// exists instead of replacing it.
func renameNoReplace(oldpath, newpath string) error {
	if err := unix.Renameat2(unix.AT_FDCWD, oldpath, unix.AT_FDCWD, newpath, unix.RENAME_NOREPLACE); err != nil {
		return fmt.Errorf("rename %s to %s: %w", oldpath, newpath, err)
	}
	return nil
}
