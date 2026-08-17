// This file is an internal test (stack-go.md §7's white-box seam) because
// loadManifestDir is the package's only loader and is unexported. It holds
// the two cases that were LoadManifest's before #568 deleted that
// duplicate: reading a manifest off disk, and the error when the file is
// not there. manifest_test.go covers everything reachable through the
// exported API and stays external.
package tool

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loaderManifest is the smallest manifest that parses and validates.
const loaderManifest = `{
	"name": "echo",
	"description": "echo the input",
	"command": "echo.sh",
	"input_schema": {"type": "object"},
	"output_schema": {"type": "object"}
}`

// TestLoadManifestDir_ReadsFile exercises the file path through the
// loader the registry uses and confirms the parsed manifest matches the
// bytes on disk. The bytes are returned unchanged, because the pin is
// taken over the canonical form rather than over them.
func TestLoadManifestDir_ReadsFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(loaderManifest), 0o600))

	md, err := loadManifestDir(dir)
	require.NoError(t, err)
	assert.Equal(t, "echo", md.manifest.Name)
	assert.Equal(t, []byte(loaderManifest), md.bytes)
}

// TestLoadManifestDir_FileMissing surfaces the read error path so the
// caller can replace the path or fail startup.
func TestLoadManifestDir_FileMissing(t *testing.T) {
	_, err := loadManifestDir(t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read manifest")
}
