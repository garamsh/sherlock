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

// ownedByProcess marks an FS whose directory belongs to the workspace
// process's own account: no exec UID has been handed the workspace, so
// nothing is chowned and directories need no shared write bit.
const ownedByProcess = -1

// File and directory modes. A handed-over directory carries the group
// write bit because its owner is the exec account while the workspace
// process, which shares its group, still has to create entries in it —
// the alternative is CAP_DAC_OVERRIDE over every file in the container.
const (
	fileMode        = 0o644
	dirModeOwned    = 0o755
	dirModeHandedTo = 0o775
)

// FS is a filesystem rooted at a workspace directory. All operations
// resolve relative paths against the root and reject paths that escape it.
// Confinement is lexical: symbolic links inside the workspace are followed
// without further checks (ADR 0005 — convenience confinement, not a trust
// boundary).
type FS struct {
	root string
	// ownerUID is the exec account the workspace directory was handed
	// to, or ownedByProcess when it was not.
	ownerUID int
}

// Open returns an FS rooted at root, creating the directory on first
// use. Until HandOver, everything the FS creates belongs to the
// workspace process's own account.
func Open(root string) (*FS, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root %q: %w", root, err)
	}
	if err := os.MkdirAll(abs, dirModeOwned); err != nil {
		return nil, fmt.Errorf("create workspace root %q: %w", abs, err)
	}
	return &FS{root: abs, ownerUID: ownedByProcess}, nil
}

// Root returns the absolute workspace root.
func (f *FS) Root() string { return f.root }

// HandOver gives the workspace directory to the agent's exec UID (ADR
// 0008, ADR 0010): a privileged server (euid 0) chowns it to uid and
// keeps the directory's write bit for its own group, so every file in
// the workspace ends up owned by the exec account — the ones exec'd
// children write natively, the ones the gRPC contract writes through the
// chown in Write. An unprivileged server owns the directory itself and
// hands over nothing.
func (f *FS) HandOver(uid int) error {
	if os.Geteuid() != 0 {
		return nil
	}
	f.ownerUID = uid
	if err := f.chownToOwner(f.root); err != nil {
		return err
	}
	if err := os.Chmod(f.root, dirModeHandedTo); err != nil {
		return fmt.Errorf("chmod %q to %o: %w", f.root, dirModeHandedTo, err)
	}
	return nil
}

// chownToOwner gives path to the exec account. It is a no-op on a
// workspace the process owns itself.
func (f *FS) chownToOwner(path string) error {
	if f.ownerUID == ownedByProcess {
		return nil
	}
	if err := os.Chown(path, f.ownerUID, -1); err != nil {
		return fmt.Errorf("chown %q to uid %d: %w", path, f.ownerUID, err)
	}
	return nil
}

// dirMode is the mode the FS creates directories with: a handed-over
// workspace needs the group write bit, one the process owns does not.
func (f *FS) dirMode() os.FileMode {
	if f.ownerUID == ownedByProcess {
		return dirModeOwned
	}
	return dirModeHandedTo
}

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

// Write writes content to the file at path, creating parent directories
// as needed and leaving the file owned by the workspace's account.
//
// The write goes through a temporary file in the target's directory and
// a rename: the file it replaces may belong to the exec account, which a
// workspace without CAP_DAC_OVERRIDE cannot open for writing, while
// renaming over it needs only the directory's write bit. The rename is
// atomic, so a reader never sees a half-written file either.
func (f *FS) Write(path string, content []byte) error {
	resolved, err := f.resolve(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(resolved)
	if err := f.mkdirAll(dir); err != nil {
		return fmt.Errorf("create parent of %q: %w", path, err)
	}
	tmp, err := os.CreateTemp(dir, ".gagent-write-*")
	if err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return fmt.Errorf("write %q: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	if err := os.Chmod(tmp.Name(), fileMode); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	if err := f.chownToOwner(tmp.Name()); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), resolved); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	return nil
}

// mkdirAll creates dir and every missing directory between it and the
// root, handing each new one to the same account as the root: a
// directory the gRPC contract creates has to be writable by exec'd
// children too.
func (f *FS) mkdirAll(dir string) error {
	rel, err := filepath.Rel(f.root, dir)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	current := f.root
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		if err := os.Mkdir(current, f.dirMode()); err != nil {
			if os.IsExist(err) {
				continue
			}
			return err
		}
		// Mkdir's mode passes through the umask, so the shared write
		// bit is set explicitly.
		if err := os.Chmod(current, f.dirMode()); err != nil {
			return err
		}
		if err := f.chownToOwner(current); err != nil {
			return err
		}
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
