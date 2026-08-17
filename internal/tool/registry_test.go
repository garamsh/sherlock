package tool_test

import (
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
