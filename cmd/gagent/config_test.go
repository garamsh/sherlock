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
	// Isolate the test from a real ~/.gagent/gagent.yaml.
	t.Setenv("HOME", t.TempDir())
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

func TestResolveConfig_HomeConfigFile(t *testing.T) {
	t.Chdir(t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".gagent"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".gagent", "gagent.yaml"),
		[]byte("data-root: /from-home\n"), 0o644))

	cmd := &cobra.Command{Use: "test"}
	addConfigFlags(cmd.Flags())
	cfg, err := resolveConfig(cmd)
	require.NoError(t, err)

	assert.Equal(t, "/from-home", cfg.DataRoot)
}

func TestResolveConfig_InvalidConfigFileFails(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"),
		[]byte("addr: [unclosed\n"), 0o644))

	_, err := resolveConfig(newTestCommand(t))
	assert.Error(t, err)
}
