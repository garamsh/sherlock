//go:build !linux

package shell

import "errors"

// RunShim reports that the resource-limit shim is Linux-only, matching
// ProcessExecutor on this platform: production deployment is Linux and
// the non-Linux build exists for tooling.
func RunShim(_ []string) error {
	return errors.New("shim: resource limits are Linux-only")
}
