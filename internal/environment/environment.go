package environment

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultRoot is the default parent directory for all environment data,
// per docs/architecture/environment.md.
const DefaultRoot = "data/envs"

// WorkspaceDir is the name of the workspace subdirectory inside an
// environment directory.
const WorkspaceDir = "workspace"

// ErrInvalidID is returned for environment IDs that are empty or could
// escape the root directory.
var ErrInvalidID = errors.New("invalid environment id")

// Environment owns one directory on disk: <root>/<id>/ containing a
// workspace/ subdirectory. The directory is the isolation unit — teardown
// removes it entirely.
type Environment struct {
	id   string
	root string
}

// Open returns the environment with the given ID under root, creating
// <root>/<id>/ and its workspace/ subdirectory on first use. Existing
// directories and their contents are left untouched.
func Open(root, id string) (*Environment, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	e := &Environment{id: id, root: root}
	if err := os.MkdirAll(e.Workspace(), 0o755); err != nil {
		return nil, fmt.Errorf("open environment %q: %w", id, err)
	}
	return e, nil
}

// ID returns the environment ID.
func (e *Environment) ID() string { return e.id }

// Dir returns the environment directory, <root>/<id>.
func (e *Environment) Dir() string { return filepath.Join(e.root, e.id) }

// Workspace returns the workspace directory, <root>/<id>/workspace.
func (e *Environment) Workspace() string { return filepath.Join(e.Dir(), WorkspaceDir) }

// Teardown removes the environment directory and everything in it. It is
// idempotent: tearing down an already-removed environment is not an error.
func (e *Environment) Teardown() error {
	if err := os.RemoveAll(e.Dir()); err != nil {
		return fmt.Errorf("teardown environment %q: %w", e.id, err)
	}
	return nil
}

// validateID rejects IDs that are empty, equal to "." or "..", or contain
// path separators or NUL bytes, so an ID can never resolve outside root.
func validateID(id string) error {
	if id == "" || id == "." || id == ".." ||
		strings.ContainsAny(id, `/\`) || strings.ContainsRune(id, 0) {
		return fmt.Errorf("%w: %q", ErrInvalidID, id)
	}
	return nil
}
