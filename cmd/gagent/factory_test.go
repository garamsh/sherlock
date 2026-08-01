package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/gateway"
)

// newStubModel returns a no-op model function suitable for tests that only
// exercise the factory wiring: nothing runs the model.
func newStubModel() func(string) agent.Model {
	return func(string) agent.Model { return nil }
}

func TestNewFactory_CarriesResolvedConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg, err := resolveConfig(newTestCommand(t))
	require.NoError(t, err)

	factory := newFactory(cfg, newStubModel(), "", nil)

	assert.Equal(t, cfg.DataRoot, factory.Root,
		"the resolved data root must reach the environment factory")
	assert.NotNil(t, factory.NewModel, "the factory must carry the model builder")
	assert.Equal(t, cfg.ExecTimeout, factory.ShellExecTimeout,
		"the resolved exec-timeout must reach the environment factory")
	assert.NotNil(t, factory.NewShellExecutor,
		"the factory must carry a shell-exec executor closure")
}

func TestNewFactory_CarriesScriptTools(t *testing.T) {
	t.Chdir(t.TempDir())
	cmd := newTestCommand(t)
	require.NoError(t, cmd.Flags().Set("config", writeToolsConfig(t)))
	cfg, err := resolveConfig(cmd)
	require.NoError(t, err)

	factory := newFactory(cfg, newStubModel(), "", nil)

	require.Len(t, factory.Tools, 1)
	assert.Equal(t, gateway.ScriptTool{
		Name:         "wordcount",
		Description:  "Count words in a text.",
		InputSchema:  "schemas/wordcount.input.json",
		OutputSchema: "schemas/wordcount.output.json",
		Exec: gateway.ScriptToolExec{
			Command:        []string{"python3", "tools/wordcount.py"},
			StdinFormat:    "json",
			StdoutFormat:   "json",
			TimeoutSeconds: 30,
			Env:            []string{"PYTHONUNBUFFERED=1"},
			Workdir:        "/srv/tools",
		},
	}, factory.Tools[0],
		"the config's tools: section must reach the environment factory")
}

// writeToolsConfig writes a gagent.yaml carrying one tools: entry and
// returns its path.
func writeToolsConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gagent.yaml")
	contents := "tools:\n" +
		"  - name: wordcount\n" +
		"    description: Count words in a text.\n" +
		"    input_schema: schemas/wordcount.input.json\n" +
		"    output_schema: schemas/wordcount.output.json\n" +
		"    exec:\n" +
		"      command: [python3, tools/wordcount.py]\n" +
		"      stdin_format: json\n" +
		"      stdout_format: json\n" +
		"      timeout_seconds: 30\n" +
		"      env: [PYTHONUNBUFFERED=1]\n" +
		"      workdir: /srv/tools\n"
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
	return path
}
