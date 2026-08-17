package config_test

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/config"
	"github.com/garamsh/gagent/internal/tool"
)

// agentFlags returns the flag set the agent subcommand resolves
// through, built the same way the command builds it, so a test reaches
// Resolve with the flag layer the entry point actually supplies.
func agentFlags(t *testing.T) *pflag.FlagSet {
	t.Helper()
	// Isolate the test from a real XDG/home gagent config.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	config.AddSharedFlags(fs)
	config.AddAgentFlags(fs)
	return fs
}

func TestResolveConfig_Defaults(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:8080", cfg.Addr)
	assert.Equal(t, "data/memory.db", cfg.MemoryPath)
	assert.Equal(t, "127.0.0.1:8081", cfg.WorkspaceAddr)
	assert.Equal(t, config.ModelConfig{
		Provider:  "mock",
		BaseURL:   "https://api.openai.com/v1",
		Model:     "gpt-4o-mini",
		APIKeyEnv: "OPENAI_API_KEY",
		Timeout:   60 * time.Second,
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"), []byte(contents), 0o600))

	cfg, err := config.Resolve(agentFlags(t))
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
		[]byte("model:\n  timeout: 30s\n"), 0o600))
	t.Setenv("GAGENT_MODEL_TIMEOUT", "5m")

	cfg, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.Equal(t, 5*time.Minute, cfg.Model.Timeout,
		"GAGENT_MODEL_TIMEOUT must override the config-file value")
}

func TestResolveConfig_ConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	contents := "addr: \":9090\"\n" +
		"memory-path: /from-file/memory.db\n" +
		"workspace-addr: 127.0.0.1:9081\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"), []byte(contents), 0o600))

	cfg, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.Equal(t, ":9090", cfg.Addr)
	assert.Equal(t, "/from-file/memory.db", cfg.MemoryPath)
	assert.Equal(t, "127.0.0.1:9081", cfg.WorkspaceAddr,
		"workspace-addr resolves from the config file")
}

func TestResolveConfig_EnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"),
		[]byte("memory-path: /from-file/memory.db\n"), 0o600))
	t.Setenv("GAGENT_MEMORY_PATH", "/from-env/memory.db")

	cfg, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.Equal(t, "/from-env/memory.db", cfg.MemoryPath)
}

func TestResolveConfig_FlagOverridesEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GAGENT_MEMORY_PATH", "/from-env/memory.db")
	fs := agentFlags(t)
	require.NoError(t, fs.Set("memory-path", "/from-flag/memory.db"))

	cfg, err := config.Resolve(fs)
	require.NoError(t, err)

	assert.Equal(t, "/from-flag/memory.db", cfg.MemoryPath)
}

func TestResolveConfig_WorkspaceAddrFlag(t *testing.T) {
	t.Chdir(t.TempDir())
	fs := agentFlags(t)
	require.NoError(t, fs.Set("workspace-addr", "127.0.0.1:9999"))

	cfg, err := config.Resolve(fs)
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:9999", cfg.WorkspaceAddr,
		"--workspace-addr overrides the config-file and default values")
}

func TestResolveConfig_XDGConfigFile(t *testing.T) {
	t.Chdir(t.TempDir())
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	require.NoError(t, os.MkdirAll(filepath.Join(xdg, "gagent"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(xdg, "gagent", "config.yaml"),
		[]byte("memory-path: /from-xdg/memory.db\n"), 0o600))

	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	config.AddSharedFlags(fs)
	config.AddAgentFlags(fs)
	cfg, err := config.Resolve(fs)
	require.NoError(t, err)

	assert.Equal(t, "/from-xdg/memory.db", cfg.MemoryPath)
}

func TestResolveConfig_CwdBeatsXDG(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	xdg := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"),
		[]byte("memory-path: /from-cwd/memory.db\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(xdg, "gagent"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(xdg, "gagent", "config.yaml"),
		[]byte("memory-path: /from-xdg/memory.db\n"), 0o600))
	fs := agentFlags(t)
	t.Setenv("XDG_CONFIG_HOME", xdg)

	cfg, err := config.Resolve(fs)
	require.NoError(t, err)

	assert.Equal(t, "/from-cwd/memory.db", cfg.MemoryPath)
}

func TestResolveConfig_ConfigFlag(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	// A cwd file exists but the flag path wins; the flag path is arbitrary.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"),
		[]byte("memory-path: /from-cwd/memory.db\n"), 0o600))
	custom := filepath.Join(t.TempDir(), "custom.yaml")
	require.NoError(t, os.WriteFile(custom, []byte("memory-path: /from-flag/memory.db\n"), 0o600))

	fs := agentFlags(t)
	require.NoError(t, fs.Set("config", custom))
	cfg, err := config.Resolve(fs)
	require.NoError(t, err)

	assert.Equal(t, "/from-flag/memory.db", cfg.MemoryPath)
}

func TestResolveConfig_ConfigFlagMissingFileFails(t *testing.T) {
	t.Chdir(t.TempDir())
	fs := agentFlags(t)
	require.NoError(t, fs.Set("config", filepath.Join(t.TempDir(), "nope.yaml")))

	_, err := config.Resolve(fs)
	assert.Error(t, err, "an explicit --config path must exist")
}

func TestResolveConfig_TLSSection(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	contents := "tls:\n" +
		"  cert: /srv/tls/server.pem\n" +
		"  key: /srv/tls/server-key.pem\n" +
		"  client-ca: /srv/tls/ca.pem\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"), []byte(contents), 0o600))

	cfg, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.Equal(t, config.TLSConfig{
		Cert:     "/srv/tls/server.pem",
		Key:      "/srv/tls/server-key.pem",
		ClientCA: "/srv/tls/ca.pem",
	}, cfg.TLS)
}

func TestResolveConfig_TLSSectionEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"),
		[]byte("tls:\n  cert: /from-file.pem\n"), 0o600))
	t.Setenv("GAGENT_TLS_CERT", "/from-env.pem")

	cfg, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.Equal(t, "/from-env.pem", cfg.TLS.Cert,
		"GAGENT_TLS_CERT must override the config-file value")
}

// TestResolveConfig_TLSSectionEnvOnly pins issue #228: the GAGENT_TLS_*
// variables resolve with no config file at all — the env layer reaches
// Unmarshal for keys that have neither a flag nor a real default.
func TestResolveConfig_TLSSectionEnvOnly(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GAGENT_TLS_CERT", "/from-env/server.pem")
	t.Setenv("GAGENT_TLS_KEY", "/from-env/server-key.pem")
	t.Setenv("GAGENT_TLS_CLIENT_CA", "/from-env/ca.pem")

	cfg, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.Equal(t, config.TLSConfig{
		Cert:     "/from-env/server.pem",
		Key:      "/from-env/server-key.pem",
		ClientCA: "/from-env/ca.pem",
	}, cfg.TLS, "env-only tls.* enables mTLS without a config file")
}

func TestResolveConfig_NoTLSSection(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.Equal(t, config.TLSConfig{}, cfg.TLS, "a missing tls: section yields the zero value")
}

// TestResolveConfig_ConfigFileMustBeOwnerOnly pins the security.md
// custody promise: a group/other-readable config file fails startup;
// 0600 and 0400 pass.
func TestResolveConfig_ConfigFileMustBeOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	path := filepath.Join(dir, "gagent.yaml")
	require.NoError(t, os.WriteFile(path, []byte("addr: 127.0.0.1:9090\n"), 0o600))

	_, err := config.Resolve(agentFlags(t))
	require.NoError(t, err, "0600 passes")

	require.NoError(t, os.Chmod(path, 0o400))
	_, err = config.Resolve(agentFlags(t))
	require.NoError(t, err, "0400 passes: the check is owner-only, not exactly 0600")

	require.NoError(t, os.Chmod(path, 0o644))
	_, err = config.Resolve(agentFlags(t))
	require.Error(t, err, "a group/other-readable config file must fail startup")
	assert.Contains(t, err.Error(), "owner-only")
}

func TestResolveConfig_ToolsPinsSection(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	contents := "tools:\n" +
		"  pins:\n" +
		"    message_send: 0f9b\n" +
		"    files: 7c31\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"), []byte(contents), 0o600))

	cfg, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.Equal(t, tool.ManifestPins{"message_send": "0f9b", "files": "7c31"}, cfg.Tools.Pins)
}

// The pin section has three states an operator can write, and #562 is
// what it cost to have only checked one of them: an empty mapping
// resolves to the same nil an absent section does, so the setting that
// looks strictest turned pinning off. Each state is measured here.

// TestResolveConfig_NoToolsSection separates "no pin set" from "a pin
// set naming nothing": the first leaves drift pinning off, the second
// names no tool the operator accepted.
func TestResolveConfig_NoToolsSection(t *testing.T) {
	t.Chdir(t.TempDir())

	cfg, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.Nil(t, cfg.Tools.Pins)
}

// TestResolveConfig_ToolsDirDefaults pins the tools directory to the
// same resolution every other setting gets. The empty-environment case
// is the one that could have changed silently: the hand-rolled lookup
// this replaced treated an empty GAGENT_TOOLS_DIR as unset, and viper
// has to agree or a deployment that exports the variable empty would
// start reading manifests from "".
// TestResolveConfig_EmptyToolsPinsSectionFails is the case #562
// reported. It must not resolve, and it must not resolve to the absent
// state: an operator who wrote the section is configuring pinning, and
// the one outcome they must not get is pinning switched off.
func TestResolveConfig_EmptyToolsPinsSectionFails(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"),
		[]byte("tools:\n  pins: {}\n"), 0o600))

	_, err := config.Resolve(agentFlags(t))

	require.Error(t, err, "an empty pin set must not resolve to pinning being off")
	assert.Contains(t, err.Error(), "tools.pins", "the error names the key the operator wrote")
	assert.Contains(t, err.Error(), "names no tool", "the error says what is wrong with it")
}

// TestResolveConfig_ToolsSectionWithoutPinsIsAbsent bounds the check to
// the section it is about: a tools: section carrying something else has
// no pin set, which is the absent state and not the empty one.
func TestResolveConfig_ToolsSectionWithoutPinsIsAbsent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"),
		[]byte("tools-dir: ./elsewhere\n"), 0o600))

	cfg, err := config.Resolve(agentFlags(t))

	require.NoError(t, err)
	assert.Nil(t, cfg.Tools.Pins)
	assert.Equal(t, "./elsewhere", cfg.ToolsDir)
}

func TestResolveConfig_ToolsDirDefaults(t *testing.T) {
	t.Chdir(t.TempDir())

	cfg, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.Equal(t, "./tools", cfg.ToolsDir)
}

func TestResolveConfig_ToolsDirEmptyEnvFallsBack(t *testing.T) {
	t.Chdir(t.TempDir())
	fs := agentFlags(t)
	t.Setenv("GAGENT_TOOLS_DIR", "")

	cfg, err := config.Resolve(fs)
	require.NoError(t, err)

	assert.Equal(t, "./tools", cfg.ToolsDir,
		"an empty GAGENT_TOOLS_DIR is unset, not a directory named the empty string")
}

func TestResolveConfig_ToolsDirEnvOverridesDefault(t *testing.T) {
	t.Chdir(t.TempDir())
	fs := agentFlags(t)
	t.Setenv("GAGENT_TOOLS_DIR", "/opt/gagent/tools")

	cfg, err := config.Resolve(fs)
	require.NoError(t, err)

	assert.Equal(t, "/opt/gagent/tools", cfg.ToolsDir)
}

func TestResolveConfig_InvalidConfigFileFails(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"),
		[]byte("addr: [unclosed\n"), 0o600))

	_, err := config.Resolve(agentFlags(t))
	assert.Error(t, err)
}

// captureSlog swaps slog.Default() to a text handler writing into the
// returned buffer for the lifetime of the test, and returns a cleanup
// that restores the previous default.
func captureSlog(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	return buf, func() { slog.SetDefault(prev) }
}

// writeAgentConfig writes an owner-only gagent.yaml into the current
// directory and returns its path.
func writeAgentConfig(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "gagent.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestResolveConfig_UnknownFileKeyIsReported(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	path := writeAgentConfig(t, dir, "addr: \"127.0.0.1:9000\"\nnonsense-key: 42\n")
	buf, restore := captureSlog(t)
	defer restore()

	_, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "key=nonsense-key",
		"the warning names the key so an operator knows what to fix")
	assert.Contains(t, buf.String(), "file="+path,
		"the warning names the file the key came from")
}

func TestResolveConfig_UnknownSectionIsReportedByItsRoot(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeAgentConfig(t, dir, "console:\n  url: \"http://127.0.0.1:9099\"\n")
	buf, restore := captureSlog(t)
	defer restore()

	_, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "key=console",
		"a section no setting reads is reported at the section, not once per leaf")
}

func TestResolveConfig_PinnedToolNamesAreNotUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeAgentConfig(t, dir, "tools:\n  pins:\n    message_send: \"abc123\"\n")
	buf, restore := captureSlog(t)
	defer restore()

	cfg, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.Equal(t, tool.ManifestPins{"message_send": "abc123"}, cfg.Tools.Pins)
	assert.NotContains(t, buf.String(), "unknown config key",
		"the tool names inside the pin set are the operator's, not keys the platform declares")
}

func TestResolveConfig_PinsTypoIsReportedByName(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeAgentConfig(t, dir, "tools:\n  pin:\n    message_send: \"abc123\"\n")
	buf, restore := captureSlog(t)
	defer restore()

	cfg, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.Empty(t, cfg.Tools.Pins,
		"pin: is not pins:, so the pin set is empty and drift pinning is off")
	assert.Contains(t, buf.String(), "key=tools.pin",
		"the key's own name is what separates an unread pin set from an absent one (ADR 0009)")
}

func TestResolveConfig_KnownKeysAndFlagsAreNotReported(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeAgentConfig(t, dir, "addr: \"127.0.0.1:9000\"\nmemory-path: \"data/memory.db\"\ntls:\n  cert: \"c.pem\"\n")
	buf, restore := captureSlog(t)
	defer restore()

	_, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.NotContains(t, buf.String(), "unknown config key",
		"the command's own flags share the viper and name no setting; only the file's keys are the operator's to fix")
}

func TestResolveConfig_NoConfigFileReportsNothing(t *testing.T) {
	t.Chdir(t.TempDir())
	buf, restore := captureSlog(t)
	defer restore()

	_, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.NotContains(t, buf.String(), "unknown config key")
}
