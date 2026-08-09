//go:build !linux

package shell

import "os/exec"

// ProtectEnviron is a no-op off Linux: the custody mechanism is
// prctl-based and production deployment is Linux-only.
func ProtectEnviron() error { return nil }

// startUsernsChild starts cmd unchanged: the user-namespace path is
// Linux-only.
func startUsernsChild(cmd *exec.Cmd) error { return cmd.Start() }
