package sqlite

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// storeFileMode is the mode of memory.db, of the -wal and -shm SQLite
// creates with the main file's mode, and of the writer lock: owner-only,
// so an account other than the agent's that reaches the volume cannot
// read the store (#998).
const storeFileMode fs.FileMode = 0o600

// restrictStore creates the memory.db at path with storeFileMode when it
// is absent, and tightens it and any -wal and -shm beside it to that mode
// when an earlier release left them wider. It changes modes only, never
// contents.
func restrictStore(path string) error {
	f, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE, storeFileMode)
	if err != nil {
		return fmt.Errorf("open %q: %v", path, err)
	}
	err = f.Chmod(storeFileMode)
	f.Close()
	if err != nil {
		return fmt.Errorf("restrict %q: %v", path, err)
	}
	// SQLite names the sidecars after the database's resolved path.
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve %q: %v", path, err)
	}
	for _, sidecar := range []string{real + "-wal", real + "-shm"} {
		if err := os.Chmod(sidecar, storeFileMode); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("restrict %q: %v", sidecar, err)
		}
	}
	return nil
}
