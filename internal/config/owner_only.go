package config

import (
	"fmt"
	"os"
)

// RequireOwnerOnly rejects a file readable by group or other. The
// platform validates and fails instead of chowning an operator-provided
// file (docs/architecture/security.md).
//
// what names the file in the error the operator reads — "config file",
// "tls: server key". Both files carry the same rule for the same
// reason: they are operator material in the runtime UID's domain
// (ADR 0017), so an agent-UID child must not be able to read them.
// 0600 and 0400 pass; 0644 fails.
func RequireOwnerOnly(what, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", what, err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("%s %q is group- or other-accessible (mode %04o); it must be owner-only (e.g. 0600 or 0400)",
			what, path, perm)
	}
	return nil
}
