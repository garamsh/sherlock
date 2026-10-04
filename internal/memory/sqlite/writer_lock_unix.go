//go:build unix

package sqlite

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// lockWriter takes the exclusive advisory lock that makes this process
// the store's one writer on this host (ADR 0002), without waiting. The
// lock is held until the returned file is closed or the process exits;
// Go opens the file close-on-exec, so no spawned tool inherits it.
func lockWriter(path string) (*os.File, error) {
	name, err := lockPath(path)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open writer lock %q: %v", name, err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, fmt.Errorf("%q: %w", name, ErrStoreLocked)
		}
		return nil, fmt.Errorf("take writer lock %q: %v", name, err)
	}
	return f, nil
}

// lockPath names the lock file beside the store's resolved real path, so
// every spelling of one store — relative, through `..`, or through a
// symlinked directory or file — contends for one lock.
func lockPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %v", path, err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return real + ".lock", nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("resolve %q: %v", path, err)
	}
	// The store does not exist yet, so only its directory resolves. A
	// name that is present here is a symlink to a missing file, whose
	// real path cannot be known before something creates it.
	if _, err := os.Lstat(abs); err == nil {
		return "", fmt.Errorf("resolve %q: symlink to a missing file", path)
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", fmt.Errorf("resolve %q: %v", path, err)
	}
	return filepath.Join(dir, filepath.Base(abs)) + ".lock", nil
}
