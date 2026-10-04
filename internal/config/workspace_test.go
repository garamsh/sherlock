package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/config"
)

// workspaceFlags returns the flag set the workspace subcommand resolves
// through: the shared --config flag plus the workspace's own settings,
// merged the way command execution merges a persistent flag into a
// subcommand's set.
func workspaceFlags(t *testing.T) *pflag.FlagSet {
	t.Helper()
	// Isolate the test from a real XDG/home config.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	fs := pflag.NewFlagSet("workspace", pflag.ContinueOnError)
	config.AddSharedFlags(fs)
	config.AddWorkspaceFlags(fs)
	return fs
}

func TestResolveWorkspaceConfig_Defaults(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg, err := config.ResolveWorkspace(workspaceFlags(t))
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:8081", cfg.Addr)
	assert.Equal(t, "data/workspace", cfg.Workspace)
	assert.Equal(t, 1001, cfg.ExecUID)
	assert.Equal(t, 30*time.Second, cfg.ExecTimeout)
}

func TestResolveWorkspaceConfig_ConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o755))
	contents := "addr: \"127.0.0.1:9091\"\n" +
		"workspace: /srv/workspace\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "configs", "workspace.yaml"), []byte(contents), 0o600))

	cfg, err := config.ResolveWorkspace(workspaceFlags(t))
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:9091", cfg.Addr)
	assert.Equal(t, "/srv/workspace", cfg.Workspace)
}

func TestResolveWorkspaceConfig_EnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "configs", "workspace.yaml"),
		[]byte("workspace: /from-file\n"), 0o600))
	t.Setenv("SHERLOCK_WORKSPACE", "/from-env")

	cfg, err := config.ResolveWorkspace(workspaceFlags(t))
	require.NoError(t, err)

	assert.Equal(t, "/from-env", cfg.Workspace)
}

func TestResolveWorkspaceConfig_FlagOverridesEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("SHERLOCK_WORKSPACE", "/from-env")
	fs := workspaceFlags(t)
	require.NoError(t, fs.Set("workspace", "/from-flag"))

	cfg, err := config.ResolveWorkspace(fs)
	require.NoError(t, err)

	assert.Equal(t, "/from-flag", cfg.Workspace)
}

func TestResolveWorkspaceConfig_XDGConfigFile(t *testing.T) {
	t.Chdir(t.TempDir())
	xdg := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(xdg, "sherlock"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(xdg, "sherlock", "workspace.yaml"),
		[]byte("workspace: /from-xdg\n"), 0o600))
	fs := workspaceFlags(t)
	t.Setenv("XDG_CONFIG_HOME", xdg)

	cfg, err := config.ResolveWorkspace(fs)
	require.NoError(t, err)

	assert.Equal(t, "/from-xdg", cfg.Workspace)
}

func TestResolveWorkspaceConfig_ConfigFlag(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o755))
	// A configs/workspace.yaml exists but the flag path wins.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "configs", "workspace.yaml"),
		[]byte("workspace: /from-cwd\n"), 0o600))
	custom := filepath.Join(t.TempDir(), "custom.yaml")
	require.NoError(t, os.WriteFile(custom, []byte("workspace: /from-flag\n"), 0o600))

	fs := workspaceFlags(t)
	require.NoError(t, fs.Set("config", custom))
	cfg, err := config.ResolveWorkspace(fs)
	require.NoError(t, err)

	assert.Equal(t, "/from-flag", cfg.Workspace)
}

func TestResolveWorkspaceConfig_UnknownFileKeyIsReported(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o755))
	path := filepath.Join(dir, "configs", "workspace.yaml")
	require.NoError(t, os.WriteFile(path,
		[]byte("addr: \"127.0.0.1:8081\"\nexec-uid-typo: 10001\n"), 0o600))
	buf, restore := captureSlog(t)
	defer restore()

	_, err := config.ResolveWorkspace(workspaceFlags(t))
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "key=exec-uid-typo",
		"the workspace resolves the same way, so it reports the same way")
	assert.Contains(t, buf.String(), "file="+path)
}

func TestResolveWorkspaceConfig_StrictConfigRefusesUnknownKey(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o755))
	path := filepath.Join(dir, "configs", "workspace.yaml")
	require.NoError(t, os.WriteFile(path,
		[]byte("addr: \"127.0.0.1:8081\"\nexec-uid-typo: 10001\n"), 0o600))

	fs := workspaceFlags(t)
	require.NoError(t, fs.Set("strict-config", "true"))
	_, err := config.ResolveWorkspace(fs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exec-uid-typo", "the refusal names the key")
	assert.Contains(t, err.Error(), path, "the refusal names the file")

	_, err = config.ResolveWorkspace(workspaceFlags(t))
	assert.NoError(t, err, "the same file starts without the flag, so the flag is what refused it")
}
