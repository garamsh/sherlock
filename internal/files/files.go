package files

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrOutsideWorkspace is returned for paths that would resolve outside the
// workspace root.
var ErrOutsideWorkspace = errors.New("path escapes workspace")

// Entry is one item in a directory listing.
type Entry struct {
	Name string `json:"name"`
	Dir  bool   `json:"dir"`
}

// FS is a filesystem rooted at a workspace directory. All operations
// resolve relative paths against the root and reject paths that escape it.
// Confinement is lexical: symbolic links inside the workspace are followed
// without further checks (ADR 0005 — convenience confinement, not a trust
// boundary).
type FS struct {
	root  string
	fsuid int
}

// Open returns an FS rooted at root, creating the directory on first
// use. A positive fsuid drops filesystem operations to that UID (Linux
// setfsuid, privileged callers only) so files created through the
// workspace gRPC server share the agent UID's ownership (ADR 0008,
// ADR 0010); fsuid <= 0 or an unprivileged caller keeps the current
// UID.
func Open(root string, fsuid int) (*FS, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root %q: %w", root, err)
	}
	if err := withFSUID(fsuid, func() error {
		return os.MkdirAll(abs, 0o755)
	}); err != nil {
		return nil, fmt.Errorf("create workspace root %q: %w", abs, err)
	}
	return &FS{root: abs, fsuid: fsuid}, nil
}

// Root returns the absolute workspace root.
func (f *FS) Root() string { return f.root }

// SecureWorkspace hands the workspace directory to the given trust-domain
// UID (ADR 0008, ADR 0010): a privileged server (euid 0) chowns it to
// uid so files written through the gRPC contract (which drop to the
// same UID) share one owner. An unprivileged server owns the directory
// itself and skips the chown.
func SecureWorkspace(path string, uid int) error {
	if os.Geteuid() != 0 {
		return nil
	}
	if err := os.Chown(path, uid, -1); err != nil {
		return fmt.Errorf("chown %q to uid %d: %w", path, uid, err)
	}
	return nil
}

// Read returns the content of the file at path.
func (f *FS) Read(path string) ([]byte, error) {
	resolved, err := f.resolve(path)
	if err != nil {
		return nil, err
	}
	var content []byte
	err = withFSUID(f.fsuid, func() error {
		content, err = os.ReadFile(resolved)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	return content, nil
}

// Write writes content to the file at path, creating parent directories as
// needed.
func (f *FS) Write(path string, content []byte) error {
	resolved, err := f.resolve(path)
	if err != nil {
		return err
	}
	return withFSUID(f.fsuid, func() error {
		if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
			return fmt.Errorf("create parent of %q: %w", path, err)
		}
		if err := os.WriteFile(resolved, content, 0o644); err != nil {
			return fmt.Errorf("write %q: %w", path, err)
		}
		return nil
	})
}

// List returns the entries of the directory at path.
func (f *FS) List(path string) ([]Entry, error) {
	resolved, err := f.resolve(path)
	if err != nil {
		return nil, err
	}
	var dirents []os.DirEntry
	err = withFSUID(f.fsuid, func() error {
		dirents, err = os.ReadDir(resolved)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list %q: %w", path, err)
	}
	entries := make([]Entry, 0, len(dirents))
	for _, d := range dirents {
		entries = append(entries, Entry{Name: d.Name(), Dir: d.IsDir()})
	}
	return entries, nil
}

// resolve joins path onto the root and rejects anything that escapes it.
func (f *FS) resolve(path string) (string, error) {
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: %q", ErrOutsideWorkspace, path)
	}
	resolved := filepath.Join(f.root, path)
	if resolved != f.root && !strings.HasPrefix(resolved, f.root+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: %q", ErrOutsideWorkspace, path)
	}
	return resolved, nil
}
