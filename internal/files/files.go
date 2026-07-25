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
// without further checks (ADR 0007 — convenience confinement, not a trust
// boundary).
type FS struct {
	root string
}

// Open returns an FS rooted at root, creating the directory on first use.
func Open(root string) (*FS, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root %q: %w", root, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("create workspace root %q: %w", abs, err)
	}
	return &FS{root: abs}, nil
}

// Root returns the absolute workspace root.
func (f *FS) Root() string { return f.root }

// Read returns the content of the file at path.
func (f *FS) Read(path string) ([]byte, error) {
	resolved, err := f.resolve(path)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(resolved)
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
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return fmt.Errorf("create parent of %q: %w", path, err)
	}
	if err := os.WriteFile(resolved, content, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	return nil
}

// List returns the entries of the directory at path.
func (f *FS) List(path string) ([]Entry, error) {
	resolved, err := f.resolve(path)
	if err != nil {
		return nil, err
	}
	dirents, err := os.ReadDir(resolved)
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
