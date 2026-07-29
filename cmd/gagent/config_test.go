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
	assert.Equal(t, defaultImage, cfg.Image)
	assert.Equal(t, defaultExecTimeout, cfg.ExecTimeout)
	assert.Equal(t, ModelConfig{
		Provider:  defaultModelProvider,
		BaseURL:   defaultModelBaseURL,
		Model:     defaultModelName,
		APIKeyEnv: defaultModelKeyEnv,
		Timeout:   defaultModelTimeout,
	}, cfg.Model)
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

func TestResolveConfig_MCPSection(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	contents := "mcp:\n" +
		"  servers:\n" +
		"    - name: time\n" +
		"      command: /usr/local/bin/time-mcp\n" +
		"      args: [\"--tz\", \"UTC\"]\n" +
		"      env: [SOME_TOKEN]\n" +
		"      timeout: 10s\n" +
		"    - name: bare\n" +
		"      command: /bin/bare-mcp\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"), []byte(contents), 0o644))

	cfg, err := resolveConfig(newTestCommand(t))
	require.NoError(t, err)

	assert.Equal(t, []MCPServerConfig{
		{
			Name:    "time",
			Command: "/usr/local/bin/time-mcp",
			Args:    []string{"--tz", "UTC"},
			Env:     []string{"SOME_TOKEN"},
			Timeout: 10 * time.Second,
		},
		{
			Name:    "bare",
			Command: "/bin/bare-mcp",
		},
	}, cfg.MCP.Servers)
}

func TestResolveConfig_ConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	contents := "addr: \":9090\"\n" +
		"data-root: /from-file\n" +
		"image: debian:13\n" +
		"exec-timeout: 45s\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"), []byte(contents), 0o644))

	cfg, err := resolveConfig(newTestCommand(t))
	require.NoError(t, err)

	assert.Equal(t, ":9090", cfg.Addr)
	assert.Equal(t, "/from-file", cfg.DataRoot)
	assert.Equal(t, "debian:13", cfg.Image)
	assert.Equal(t, 45*time.Second, cfg.ExecTimeout)
}

func TestResolveConfig_EnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"),
		[]byte("data-root: /from-file\nimage: debian:13\n"), 0o644))
	t.Setenv("GAGENT_DATA_ROOT", "/from-env")

	cfg, err := resolveConfig(newTestCommand(t))
	require.NoError(t, err)

	assert.Equal(t, "/from-env", cfg.DataRoot)
	assert.Equal(t, "debian:13", cfg.Image)
}

func TestResolveConfig_FlagOverridesEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GAGENT_DATA_ROOT", "/from-env")
	cmd := newTestCommand(t)
	require.NoError(t, cmd.Flags().Set("data-root", "/from-flag"))
	require.NoError(t, cmd.Flags().Set("exec-timeout", "1m"))

	cfg, err := resolveConfig(cmd)
	require.NoError(t, err)

	assert.Equal(t, "/from-flag", cfg.DataRoot)
	assert.Equal(t, time.Minute, cfg.ExecTimeout)
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
		[]byte("data-root: /from-xdg\nimage: debian:13\n"), 0o644))
	cmd := newTestCommand(t)
	t.Setenv("XDG_CONFIG_HOME", xdg)

	cfg, err := resolveConfig(cmd)
	require.NoError(t, err)

	assert.Equal(t, "/from-cwd", cfg.DataRoot)
	assert.Equal(t, defaultImage, cfg.Image, "XDG file is skipped once the cwd file is found")
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

func TestResolveConfig_InvalidConfigFileFails(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"),
		[]byte("addr: [unclosed\n"), 0o644))

	_, err := resolveConfig(newTestCommand(t))
	assert.Error(t, err)
}
