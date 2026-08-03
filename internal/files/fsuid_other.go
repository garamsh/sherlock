//go:build !linux

package files

// withFSUID runs fn unchanged: setfsuid is Linux-specific, so non-Linux
// platforms keep the current UID (the unprivileged behavior).
func withFSUID(_ int, fn func() error) error {
	return fn()
}
