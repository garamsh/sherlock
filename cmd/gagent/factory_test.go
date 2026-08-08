package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/tool"
	"github.com/garamsh/gagent/internal/tool/script"
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

	factory, err := newFactory(cfg, newStubModel(), memory.NewHashingEmbedder(), memory.NewConcatSummarizer(), "", nil)
	require.NoError(t, err)

	assert.Equal(t, cfg.DataRoot, factory.Root,
		"the resolved data root must reach the environment factory")
	assert.NotNil(t, factory.NewModel, "the factory must carry the model builder")
	assert.NotNil(t, factory.Embedder,
		"the resolved embedder must reach the environment factory")
	assert.NotNil(t, factory.Summarizer,
		"the resolved summarizer must reach the environment factory")
	assert.Equal(t, cfg.ExecTimeout, factory.ShellExecTimeout,
		"the resolved exec-timeout must reach the environment factory")
	assert.NotNil(t, factory.NewShellExecutor,
		"the factory must carry a shell-exec executor closure")
}

func TestNewFactory_CarriesScriptTools(t *testing.T) {
	t.Chdir(t.TempDir())
	cmd := newTestCommand(t)
	configPath, inputSchema, outputSchema := writeToolsConfig(t)
	require.NoError(t, cmd.Flags().Set("config", configPath))
	cfg, err := resolveConfig(cmd)
	require.NoError(t, err)

	factory, err := newFactory(cfg, newStubModel(), memory.NewHashingEmbedder(), memory.NewConcatSummarizer(), "", nil)
	require.NoError(t, err)

	require.Len(t, factory.Tools, 1)
	assert.Equal(t, script.Config{
		Name:         "wordcount",
		Description:  "Count words in a text.",
		InputSchema:  inputSchema,
		OutputSchema: outputSchema,
		Exec: script.ExecConfig{
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

func TestNewFactory_InvalidScriptToolFailsStartup(t *testing.T) {
	t.Chdir(t.TempDir())
	// The tool name collides with the built-in memory tool; both entry
	// points share newFactory, so both fail startup with this error.
	cmd := newTestCommand(t)
	require.NoError(t, cmd.Flags().Set("config", writeCollidingToolsConfig(t)))
	cfg, err := resolveConfig(cmd)
	require.NoError(t, err)

	_, err = newFactory(cfg, newStubModel(), memory.NewHashingEmbedder(), memory.NewConcatSummarizer(), "", nil)
	require.ErrorIs(t, err, tool.ErrDuplicateTool,
		"a script tool named after a built-in must fail startup")
	assert.Contains(t, err.Error(), `"memory"`,
		"the startup error must name the offending tool")
}

// writeToolsConfig writes a gagent.yaml carrying one tools: entry along
// with the schema files it references, and returns the config path and
// the two schema paths.
func writeToolsConfig(t *testing.T) (configPath, inputSchema, outputSchema string) {
	t.Helper()
	dir := t.TempDir()
	inputSchema = filepath.Join(dir, "wordcount.input.json")
	outputSchema = filepath.Join(dir, "wordcount.output.json")
	require.NoError(t, os.WriteFile(inputSchema, []byte(
		`{"type":"object","properties":{"text":{"type":"string"}}}`), 0o644))
	require.NoError(t, os.WriteFile(outputSchema, []byte(
		`{"type":"object","properties":{"count":{"type":"integer"}}}`), 0o644))
	configPath = filepath.Join(dir, "gagent.yaml")
	contents := "tools:\n" +
		"  - name: wordcount\n" +
		"    description: Count words in a text.\n" +
		"    input_schema: " + inputSchema + "\n" +
		"    output_schema: " + outputSchema + "\n" +
		"    exec:\n" +
		"      command: [python3, tools/wordcount.py]\n" +
		"      stdin_format: json\n" +
		"      stdout_format: json\n" +
		"      timeout_seconds: 30\n" +
		"      env: [PYTHONUNBUFFERED=1]\n" +
		"      workdir: /srv/tools\n"
	require.NoError(t, os.WriteFile(configPath, []byte(contents), 0o644))
	return configPath, inputSchema, outputSchema
}

// writeCollidingToolsConfig writes a gagent.yaml whose one tools: entry
// is named after the built-in memory tool, and returns its path.
func writeCollidingToolsConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.json")
	require.NoError(t, os.WriteFile(schema, []byte(`{"type":"object"}`), 0o644))
	path := filepath.Join(dir, "gagent.yaml")
	contents := "tools:\n" +
		"  - name: memory\n" +
		"    description: Collides with the built-in memory tool.\n" +
		"    input_schema: " + schema + "\n" +
		"    output_schema: " + schema + "\n" +
		"    exec:\n" +
		"      command: [true]\n"
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
	return path
}
