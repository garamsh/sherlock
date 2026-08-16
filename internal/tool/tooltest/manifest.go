package tooltest

import (
	"os"
	"path/filepath"
	"testing"
)

// ReadManifest returns the bytes of the named tool's manifest, the
// document tool.NewRegistry loads at startup and validates calls
// against (ADR 0017, ADR 0009). Tests take the contract from here so a
// guard cannot report on a copy no running process reads.
//
// name is the tool directory name, so ReadManifest(t, "message_send")
// reads tools/message_send/manifest.json.
func ReadManifest(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join(repoRoot(t), "tools", name, "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s manifest: %v", name, err)
	}
	return data
}

// repoRoot returns the module root for the current test process. A test
// binary runs in its own package directory, so the tools/ tree is
// reached by walking up to the directory holding go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate module root from %s", dir)
		}
		dir = parent
	}
}
