//go:build !linux

package main

import "errors"

// renameNoReplace fails closed off Linux: a rename that refuses an
// existing name is renameat2(RENAME_NOREPLACE), and the agent image the
// tool ships in is Linux.
func renameNoReplace(oldpath, newpath string) error {
	return errors.New("rename without replace is unsupported on this platform")
}
