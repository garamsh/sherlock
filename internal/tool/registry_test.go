package tool_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/tool"
)

// writeToolDir scaffolds tools/<dir>/manifest.json under root, declaring
// the given tool name.
func writeToolDir(t *testing.T, root, dir, name string) {
	t.Helper()
	toolDir := filepath.Join(root, dir)
	require.NoError(t, os.MkdirAll(toolDir, 0o755))
	manifest := fmt.Sprintf(`{
	"name": %q,
	"description": "d",
	"command": "run.sh",
	"input_schema": {"type": "object"},
	"output_schema": {"type": "object"}
}`, name)
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, "manifest.json"),
		[]byte(manifest), 0o600))
}

// TestNewRegistry_RejectsDuplicateToolNames pins the identity rule: a
// tool's name is what a call addresses, so two manifests claiming one
// name is an ambiguous install and startup refuses it. Without the
// refusal the load is last-wins and which tool answers a call depends on
// the order the tools directory is walked in.
//
// Required is empty deliberately. The default set expects message_send,
// which this fixture does not hold, so a registry built with the default
// would fail for that reason instead — and the assertion would then hold
// with the duplicate-name guard deleted, which is the mutant this test
// exists to kill (survey M10). Asserting the message rather than only
// the error is the other half of that: it is what separates this refusal
// from any other startup failure over the same fixture.
func TestNewRegistry_RejectsDuplicateToolNames(t *testing.T) {
	root := t.TempDir()
	writeToolDir(t, root, "alpha", "echo")
	writeToolDir(t, root, "beta", "echo")

	_, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Required: []string{},
		Logger:   slog.New(slog.DiscardHandler),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `duplicate tool name "echo"`)
}

// TestNewRegistry_DistinctNamesLoad is the control: the same fixture with
// two names loads both tools. Without it the case above would also pass
// against a registry that refused every two-directory tree.
func TestNewRegistry_DistinctNamesLoad(t *testing.T) {
	root := t.TempDir()
	writeToolDir(t, root, "alpha", "echo")
	writeToolDir(t, root, "beta", "reverse")

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Required: []string{},
		Logger:   slog.New(slog.DiscardHandler),
	})
	require.NoError(t, err)

	names := make([]string, 0, 2)
	for _, d := range reg.List() {
		names = append(names, d.Name)
	}
	assert.ElementsMatch(t, []string{"echo", "reverse"}, names)
}

// relativeToolsDir puts the process in <root>/workdir and returns root,
// so a test can configure "../tools" and know the answer: a working
// directory that is not the resolved path's parent is what separates
// reporting the resolved path from reporting the configured one, and
// what separates a cleaned join from a literal one.
func relativeToolsDir(t *testing.T) (root string) {
	t.Helper()
	workdir := filepath.Join(t.TempDir(), "workdir")
	require.NoError(t, os.MkdirAll(workdir, 0o755))
	t.Chdir(workdir)
	wd, err := os.Getwd()
	require.NoError(t, err)
	return filepath.Dir(wd)
}

// TestNewRegistry_MissingDirNamesResolvedPath holds the failure to the
// path it actually looked at. A relative tools directory means nothing
// without the working directory the process runs in, so an operator
// reading `tools directory "./tools" does not exist` cannot tell whether
// the tree is missing or the process started somewhere unexpected
// (issue #771).
//
// The configured value is relative and the working directory is not the
// resolved path's parent, so the resolved path is a string the
// configured one does not contain: an implementation that reverts to
// printing the raw configured value fails here, and so does one that
// joins without cleaning, which would report <root>/workdir/../tools.
// Required is empty for the reason TestNewRegistry_RejectsDuplicateToolNames
// gives: the default set would fail this fixture for its own reason.
func TestNewRegistry_MissingDirNamesResolvedPath(t *testing.T) {
	root := relativeToolsDir(t)

	_, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      "../tools",
		Required: []string{},
		Logger:   slog.New(slog.DiscardHandler),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), filepath.Join(root, "tools"),
		"the message names the absolute path the registry looked at")
	assert.Contains(t, err.Error(), `"../tools"`,
		"and keeps the configured value, which is what an operator edits")
}

// TestNewRegistry_UnreadableDirNamesResolvedPath is the same guarantee on
// the sibling wrap: a tools directory that exists and cannot be read as
// one fails through a different branch, and an operator diagnosing it
// needs the same two paths. The fixture is a regular file where the
// directory belongs, which fails with ENOTDIR rather than ENOENT — a
// condition no privilege bypasses, unlike an unreadable mode.
func TestNewRegistry_UnreadableDirNamesResolvedPath(t *testing.T) {
	root := relativeToolsDir(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "tools"), []byte("not a directory"), 0o600))

	_, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      "../tools",
		Required: []string{},
		Logger:   slog.New(slog.DiscardHandler),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), filepath.Join(root, "tools"),
		"the message names the absolute path the registry looked at")
	assert.Contains(t, err.Error(), `"../tools"`,
		"and keeps the configured value, which is what an operator edits")
}

// TestNewRegistry_StartupLogNamesResolvedToolsDir covers the success
// path, where no error is emitted and the resolved path is otherwise
// unreported. A tree mounted at the wrong path loads zero tools and
// fails on the required set, which names no directory, so the startup
// line is what tells an operator where the registry read from.
func TestNewRegistry_StartupLogNamesResolvedToolsDir(t *testing.T) {
	root := relativeToolsDir(t)
	writeToolDir(t, filepath.Join(root, "tools"), "alpha", "echo")

	var buf bytes.Buffer
	_, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      "../tools",
		Required: []string{},
		Logger:   slog.New(slog.NewJSONHandler(&buf, nil)),
	})
	require.NoError(t, err)

	line := logLine(t, &buf, "tool registry loaded")
	assert.Equal(t, filepath.Join(root, "tools"), line["tools_dir_resolved"],
		"the startup line names the absolute path the registry read from")
	assert.Equal(t, "../tools", line["tools_dir"],
		"and keeps the configured value beside it")
	assert.Equal(t, float64(1), line["tools"])
}

// logLine returns the first JSON log record in buf whose msg is want.
func logLine(t *testing.T, buf *bytes.Buffer, want string) map[string]any {
	t.Helper()
	for _, raw := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var entry map[string]any
		require.NoError(t, json.Unmarshal(raw, &entry))
		if entry["msg"] == want {
			return entry
		}
	}
	t.Fatalf("no log record with msg %q in:\n%s", want, buf.String())
	return nil
}
