package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/environment"
	"github.com/garamsh/gagent/internal/gateway"
)

// newTestCommand returns a bare command carrying the shared config flags,
// so resolveConfig can be exercised without running a subcommand.
func newTestCommand(t *testing.T) *cobra.Command {
	t.Helper()
	// Isolate the test from a real XDG/home gagent config.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cmd := &cobra.Command{Use: "test"}
	addConfigFlags(cmd.Flags())
	return cmd
}

func TestResolveConfig_Defaults(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg, err := resolveConfig(newTestCommand(t))
	require.NoError(t, err)

	assert.Equal(t, defaultAddr, cfg.Addr)
	assert.Equal(t, environment.DefaultRoot, cfg.DataRoot)
	assert.Equal(t, defaultExecTimeout, cfg.ExecTimeout)
	assert.Equal(t, ModelConfig{
		Provider:  defaultModelProvider,
		BaseURL:   defaultModelBaseURL,
		Model:     defaultModelName,
		APIKeyEnv: defaultModelKeyEnv,
		Timeout:   defaultModelTimeout,
	}, cfg.Model)
	assert.Equal(t, ModelConfig{
		Provider:  defaultEmbedderProvider,
		BaseURL:   defaultEmbedderBaseURL,
		Model:     defaultEmbedderModel,
		APIKeyEnv: defaultEmbedderKeyEnv,
		Timeout:   defaultEmbedderTimeout,
	}, cfg.Embedder)
}

func TestResolveConfig_ModelSection(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	contents := "model:\n" +
		"  provider: openai\n" +
		"  base-url: http://localhost:11434/v1\n" +
		"  model: llama3.2\n" +
		"  api-key-env: OLLAMA_KEY\n" +
		"  timeout: 120s\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"), []byte(contents), 0o644))

	cfg, err := resolveConfig(newTestCommand(t))
	require.NoError(t, err)

	assert.Equal(t, "openai", cfg.Model.Provider)
	assert.Equal(t, "http://localhost:11434/v1", cfg.Model.BaseURL)
	assert.Equal(t, "llama3.2", cfg.Model.Model)
	assert.Equal(t, "OLLAMA_KEY", cfg.Model.APIKeyEnv)
	assert.Equal(t, 120*time.Second, cfg.Model.Timeout)
}

func TestResolveConfig_EmbedderSection(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	contents := "embedder:\n" +
		"  provider: openai\n" +
		"  base-url: http://localhost:11434/v1\n" +
		"  model: nomic-embed-text\n" +
		"  api-key-env: OLLAMA_KEY\n" +
		"  timeout: 120s\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"), []byte(contents), 0o644))

	cfg, err := resolveConfig(newTestCommand(t))
	require.NoError(t, err)

	assert.Equal(t, "openai", cfg.Embedder.Provider)
	assert.Equal(t, "http://localhost:11434/v1", cfg.Embedder.BaseURL)
	assert.Equal(t, "nomic-embed-text", cfg.Embedder.Model)
	assert.Equal(t, "OLLAMA_KEY", cfg.Embedder.APIKeyEnv)
	assert.Equal(t, 120*time.Second, cfg.Embedder.Timeout)
}

func TestResolveConfig_ModelTimeoutEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"),
		[]byte("model:\n  timeout: 30s\n"), 0o644))
	t.Setenv("GAGENT_MODEL_TIMEOUT", "5m")

	cfg, err := resolveConfig(newTestCommand(t))
	require.NoError(t, err)

	assert.Equal(t, 5*time.Minute, cfg.Model.Timeout,
		"GAGENT_MODEL_TIMEOUT must override the config-file value")
}

func TestResolveConfig_ConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	contents := "addr: \":9090\"\n" +
		"data-root: /from-file\n" +
		"exec-timeout: 5s\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"), []byte(contents), 0o644))

	cfg, err := resolveConfig(newTestCommand(t))
	require.NoError(t, err)

	assert.Equal(t, ":9090", cfg.Addr)
	assert.Equal(t, "/from-file", cfg.DataRoot)
	assert.Equal(t, 5*time.Second, cfg.ExecTimeout,
		"exec-timeout resolves from the config file")
}

func TestResolveConfig_EnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"),
		[]byte("data-root: /from-file\n"), 0o644))
	t.Setenv("GAGENT_DATA_ROOT", "/from-env")

	cfg, err := resolveConfig(newTestCommand(t))
	require.NoError(t, err)

	assert.Equal(t, "/from-env", cfg.DataRoot)
}

func TestResolveConfig_FlagOverridesEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GAGENT_DATA_ROOT", "/from-env")
	cmd := newTestCommand(t)
	require.NoError(t, cmd.Flags().Set("data-root", "/from-flag"))

	cfg, err := resolveConfig(cmd)
	require.NoError(t, err)

	assert.Equal(t, "/from-flag", cfg.DataRoot)
}

func TestResolveConfig_XDGConfigFile(t *testing.T) {
	t.Chdir(t.TempDir())
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	require.NoError(t, os.MkdirAll(filepath.Join(xdg, "gagent"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(xdg, "gagent", "config.yaml"),
		[]byte("data-root: /from-xdg\n"), 0o644))

	cmd := &cobra.Command{Use: "test"}
	addConfigFlags(cmd.Flags())
	cfg, err := resolveConfig(cmd)
	require.NoError(t, err)

	assert.Equal(t, "/from-xdg", cfg.DataRoot)
}

func TestResolveConfig_CwdBeatsXDG(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	xdg := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"),
		[]byte("data-root: /from-cwd\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(xdg, "gagent"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(xdg, "gagent", "config.yaml"),
		[]byte("data-root: /from-xdg\n"), 0o644))
	cmd := newTestCommand(t)
	t.Setenv("XDG_CONFIG_HOME", xdg)

	cfg, err := resolveConfig(cmd)
	require.NoError(t, err)

	assert.Equal(t, "/from-cwd", cfg.DataRoot)
}

func TestResolveConfig_ConfigFlag(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	// A cwd file exists but the flag path wins; the flag path is arbitrary.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"),
		[]byte("data-root: /from-cwd\n"), 0o644))
	custom := filepath.Join(t.TempDir(), "custom.yaml")
	require.NoError(t, os.WriteFile(custom, []byte("data-root: /from-flag\n"), 0o644))

	cmd := newTestCommand(t)
	require.NoError(t, cmd.Flags().Set("config", custom))
	cfg, err := resolveConfig(cmd)
	require.NoError(t, err)

	assert.Equal(t, "/from-flag", cfg.DataRoot)
}

func TestResolveConfig_ConfigFlagMissingFileFails(t *testing.T) {
	t.Chdir(t.TempDir())
	cmd := newTestCommand(t)
	require.NoError(t, cmd.Flags().Set("config", filepath.Join(t.TempDir(), "nope.yaml")))

	_, err := resolveConfig(cmd)
	assert.Error(t, err, "an explicit --config path must exist")
}

func TestResolveConfig_ToolsSection(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"), []byte(contents), 0o644))

	cfg, err := resolveConfig(newTestCommand(t))
	require.NoError(t, err)

	require.Len(t, cfg.Tools, 1)
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
	}, cfg.Tools[0])
	// timeout_seconds is an int; the gateway converts it to
	// time.Duration at registration (30s, not 30ns).
	assert.Equal(t, 30*time.Second, time.Duration(cfg.Tools[0].Exec.TimeoutSeconds)*time.Second)
}

func TestResolveConfig_NoToolsSection(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg, err := resolveConfig(newTestCommand(t))
	require.NoError(t, err)

	assert.Empty(t, cfg.Tools, "a missing tools: section yields an empty slice")
}

func TestResolveConfig_ConsoleSection(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	contents := "console:\n" +
		"  url: https://console.internal:9443/messages\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"), []byte(contents), 0o644))

	cfg, err := resolveConfig(newTestCommand(t))
	require.NoError(t, err)

	assert.Equal(t, ConsoleConfig{URL: "https://console.internal:9443/messages"}, cfg.Console)
}

func TestResolveConfig_TLSSection(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	contents := "tls:\n" +
		"  cert: /srv/tls/server.pem\n" +
		"  key: /srv/tls/server-key.pem\n" +
		"  client-ca: /srv/tls/ca.pem\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"), []byte(contents), 0o644))

	cfg, err := resolveConfig(newTestCommand(t))
	require.NoError(t, err)

	assert.Equal(t, TLSConfig{
		Cert:     "/srv/tls/server.pem",
		Key:      "/srv/tls/server-key.pem",
		ClientCA: "/srv/tls/ca.pem",
	}, cfg.TLS)
}

func TestResolveConfig_TLSSectionEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"),
		[]byte("tls:\n  cert: /from-file.pem\n"), 0o644))
	t.Setenv("GAGENT_TLS_CERT", "/from-env.pem")

	cfg, err := resolveConfig(newTestCommand(t))
	require.NoError(t, err)

	assert.Equal(t, "/from-env.pem", cfg.TLS.Cert,
		"GAGENT_TLS_CERT must override the config-file value")
}

func TestResolveConfig_NoTLSSection(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg, err := resolveConfig(newTestCommand(t))
	require.NoError(t, err)

	assert.Equal(t, TLSConfig{}, cfg.TLS, "a missing tls: section yields the zero value")
}

func TestResolveConfig_InvalidConfigFileFails(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"),
		[]byte("addr: [unclosed\n"), 0o644))

	_, err := resolveConfig(newTestCommand(t))
	assert.Error(t, err)
}
