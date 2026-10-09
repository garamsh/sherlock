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

	"github.com/garamsh/sherlock/internal/config"
	"github.com/garamsh/sherlock/internal/tool"
)

// agentFlags returns the flag set the agent subcommand resolves
// through, built the same way the command builds it, so a test reaches
// Resolve with the flag layer the entry point actually supplies.
func agentFlags(t *testing.T) *pflag.FlagSet {
	t.Helper()
	// Isolate the test from a real XDG/home sherlock config.
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
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o755))
	contents := "model:\n" +
		"  provider: openai-compatible\n" +
		"  base-url: http://localhost:11434/v1\n" +
		"  model: llama3.2\n" +
		"  api-key-env: OLLAMA_KEY\n" +
		"  timeout: 120s\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "configs", "agent.yaml"), []byte(contents), 0o600))

	cfg, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.Equal(t, "openai-compatible", cfg.Model.Provider)
	assert.Equal(t, "http://localhost:11434/v1", cfg.Model.BaseURL)
	assert.Equal(t, "llama3.2", cfg.Model.Model)
	assert.Equal(t, "OLLAMA_KEY", cfg.Model.APIKeyEnv)
	assert.Equal(t, 120*time.Second, cfg.Model.Timeout)
}

func TestResolveConfig_EmbeddingSectionAndEnv(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "configs", "agent.yaml"),
		[]byte("embedding:\n  base-url: http://127.0.0.1:8090/v1\n  model: BAAI/bge-base-en-v1.5\n"), 0o600))
	t.Setenv("SHERLOCK_EMBEDDING_API_KEY_ENV", "EMBED_KEY")

	cfg, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.Equal(t, config.EmbeddingConfig{
		BaseURL:   "http://127.0.0.1:8090/v1",
		Model:     "BAAI/bge-base-en-v1.5",
		APIKeyEnv: "EMBED_KEY",
	}, cfg.Embedding, "the file sets the endpoint and SHERLOCK_EMBEDDING_API_KEY_ENV, absent from it, reaches the key")
}

func TestResolveConfig_ModelTimeoutEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "configs", "agent.yaml"),
		[]byte("model:\n  timeout: 30s\n"), 0o600))
	t.Setenv("SHERLOCK_MODEL_TIMEOUT", "5m")

	cfg, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.Equal(t, 5*time.Minute, cfg.Model.Timeout,
		"SHERLOCK_MODEL_TIMEOUT must override the config-file value")
}

func TestResolveConfig_ConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o755))
	contents := "addr: \":9090\"\n" +
		"memory-path: /from-file/memory.db\n" +
		"workspace-addr: 127.0.0.1:9081\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "configs", "agent.yaml"), []byte(contents), 0o600))

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
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "configs", "agent.yaml"),
		[]byte("memory-path: /from-file/memory.db\n"), 0o600))
	t.Setenv("SHERLOCK_MEMORY_PATH", "/from-env/memory.db")

	cfg, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.Equal(t, "/from-env/memory.db", cfg.MemoryPath)
}

func TestResolveConfig_FlagOverridesEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("SHERLOCK_MEMORY_PATH", "/from-env/memory.db")
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
	require.NoError(t, os.MkdirAll(filepath.Join(xdg, "sherlock"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(xdg, "sherlock", "config.yaml"),
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
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o755))
	xdg := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "configs", "agent.yaml"),
		[]byte("memory-path: /from-cwd/memory.db\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(xdg, "sherlock"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(xdg, "sherlock", "config.yaml"),
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
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o755))
	// A configs/agent.yaml file exists but the flag path wins; the flag path is arbitrary.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "configs", "agent.yaml"),
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
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o755))
	contents := "tls:\n" +
		"  cert: /srv/tls/server.pem\n" +
		"  key: /srv/tls/server-key.pem\n" +
		"  client-ca: /srv/tls/ca.pem\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "configs", "agent.yaml"), []byte(contents), 0o600))

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
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "configs", "agent.yaml"),
		[]byte("tls:\n  cert: /from-file.pem\n"), 0o600))
	t.Setenv("SHERLOCK_TLS_CERT", "/from-env.pem")

	cfg, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.Equal(t, "/from-env.pem", cfg.TLS.Cert,
		"SHERLOCK_TLS_CERT must override the config-file value")
}

// TestResolveConfig_TLSSectionEnvOnly pins issue #228: the SHERLOCK_TLS_*
// variables resolve with no config file at all — the env layer reaches
// Unmarshal for keys that have neither a flag nor a real default.
func TestResolveConfig_TLSSectionEnvOnly(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("SHERLOCK_TLS_CERT", "/from-env/server.pem")
	t.Setenv("SHERLOCK_TLS_KEY", "/from-env/server-key.pem")
	t.Setenv("SHERLOCK_TLS_CLIENT_CA", "/from-env/ca.pem")

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
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o755))
	path := filepath.Join(dir, "configs", "agent.yaml")
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
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o755))
	contents := "tools:\n" +
		"  pins:\n" +
		"    message_send: 0f9b\n" +
		"    files: 7c31\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "configs", "agent.yaml"), []byte(contents), 0o600))

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
// this replaced treated an empty SHERLOCK_TOOLS_DIR as unset, and viper
// has to agree or a deployment that exports the variable empty would
// start reading manifests from "".
// TestResolveConfig_EmptyToolsPinsSectionFails is the case #562
// reported. It must not resolve, and it must not resolve to the absent
// state: an operator who wrote the section is configuring pinning, and
// the one outcome they must not get is pinning switched off.
func TestResolveConfig_EmptyToolsPinsSectionFails(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "configs", "agent.yaml"),
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
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "configs", "agent.yaml"),
		[]byte("tools-dir: ./elsewhere\n"), 0o600))

	cfg, err := config.Resolve(agentFlags(t))

	require.NoError(t, err)
	assert.Nil(t, cfg.Tools.Pins)
	assert.Equal(t, "./elsewhere", cfg.ToolsDir)
}

// TestResolveConfig_ToolsPinsEnvIsRefused is the case #745 reported.
// Every key binds to the environment, so SHERLOCK_TOOLS_PINS made
// tools.pins present and unmarshalled its scalar to an empty map: an
// operator who named two tools was told they had named none.
//
// The control is the same directory and the same file one step earlier:
// the pin set resolves, and the neighbouring tools key resolves through
// the environment, so what the refusal answers is the variable and not
// the pin set's content or the environment layer.
func TestResolveConfig_ToolsPinsEnvIsRefused(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "configs", "agent.yaml"),
		[]byte("tools:\n  pins:\n    message_send: 0f9b\n    files: 7c31\n"), 0o600))
	t.Setenv("SHERLOCK_TOOLS_DIR", "/opt/sherlock/tools")

	cfg, err := config.Resolve(agentFlags(t))

	require.NoError(t, err)
	require.Equal(t, tool.ManifestPins{"message_send": "0f9b", "files": "7c31"}, cfg.Tools.Pins,
		"the pin set is accepted from the file: the channel is what the refusal is about, not the content")
	require.Equal(t, "/opt/sherlock/tools", cfg.ToolsDir,
		"SHERLOCK_TOOLS_DIR still reaches its setting: the refusal is this key's, not the environment layer's")

	t.Setenv("SHERLOCK_TOOLS_PINS", "files=7c31,message_send=0f9b")

	_, err = config.Resolve(agentFlags(t))

	require.Error(t, err, "the environment form of the pin set must not resolve")
	assert.Contains(t, err.Error(), "SHERLOCK_TOOLS_PINS",
		"the error names the variable the operator set")
	assert.Contains(t, err.Error(), "cannot express tools.pins",
		"the error says the environment cannot carry this key")
	assert.Contains(t, err.Error(), "config file",
		"the error names where the pin set goes instead")
	assert.NotContains(t, err.Error(), "names no tool",
		"the operator named two tools; #745 is that the empty-section message told them they named none")
}

// TestResolveConfig_EmptyToolsPinsEnvIsRefused holds the refusal to the
// variable rather than to its value. An empty one resolved before, and
// left pinning off for an operator who had named the variable — the
// state that reads as configured and is not, which is what #562 cost.
func TestResolveConfig_EmptyToolsPinsEnvIsRefused(t *testing.T) {
	t.Chdir(t.TempDir())

	cfg, err := config.Resolve(agentFlags(t))

	require.NoError(t, err)
	require.Nil(t, cfg.Tools.Pins,
		"with no variable and no section this is the absent state, and it resolves")

	t.Setenv("SHERLOCK_TOOLS_PINS", "")

	_, err = config.Resolve(agentFlags(t))

	require.Error(t, err, "an empty SHERLOCK_TOOLS_PINS must not resolve to pinning being off")
	assert.Contains(t, err.Error(), "SHERLOCK_TOOLS_PINS")
	assert.Contains(t, err.Error(), "config file",
		"the error names where the pin set goes instead")
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
	t.Setenv("SHERLOCK_TOOLS_DIR", "")

	cfg, err := config.Resolve(fs)
	require.NoError(t, err)

	assert.Equal(t, "./tools", cfg.ToolsDir,
		"an empty SHERLOCK_TOOLS_DIR is unset, not a directory named the empty string")
}

func TestResolveConfig_ToolsDirEnvOverridesDefault(t *testing.T) {
	t.Chdir(t.TempDir())
	fs := agentFlags(t)
	t.Setenv("SHERLOCK_TOOLS_DIR", "/opt/sherlock/tools")

	cfg, err := config.Resolve(fs)
	require.NoError(t, err)

	assert.Equal(t, "/opt/sherlock/tools", cfg.ToolsDir)
}

func TestResolveConfig_InvalidConfigFileFails(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "configs", "agent.yaml"),
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

// writeAgentConfig writes an owner-only agent.yaml under the current
// directory's configs/ and returns its path.
func writeAgentConfig(t *testing.T, dir, body string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o755))
	path := filepath.Join(dir, "configs", "agent.yaml")
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

// strictAgentFlags returns agentFlags with --strict-config set, read back
// so a flag that failed to set cannot pass as the strict case.
func strictAgentFlags(t *testing.T) *pflag.FlagSet {
	t.Helper()
	fs := agentFlags(t)
	require.NoError(t, fs.Set("strict-config", "true"))
	strict, err := fs.GetBool("strict-config")
	require.NoError(t, err)
	require.True(t, strict)
	return fs
}

func TestResolveConfig_StrictConfigRefusesUnknownTopLevelKey(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	path := writeAgentConfig(t, dir, "addr: \"127.0.0.1:9000\"\nnonsense-key: 42\n")

	_, err := config.Resolve(strictAgentFlags(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonsense-key", "the refusal names the key")
	assert.Contains(t, err.Error(), path, "the refusal names the file")

	_, err = config.Resolve(agentFlags(t))
	assert.NoError(t, err, "the same file starts without the flag, so the flag is what refused it")
}

func TestResolveConfig_StrictConfigRefusesUnknownNestedKey(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	path := writeAgentConfig(t, dir, "model:\n  provider: \"mock\"\n  temprature: 0.2\n")

	_, err := config.Resolve(strictAgentFlags(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model.temprature", "the refusal names the nested key by its full path")
	assert.Contains(t, err.Error(), path, "the refusal names the file")

	_, err = config.Resolve(agentFlags(t))
	assert.NoError(t, err, "the same file starts without the flag, so the flag is what refused it")
}

func TestResolveConfig_StrictConfigStartsOnFullyReadFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeAgentConfig(t, dir, "addr: \"127.0.0.1:9000\"\nmodel:\n  provider: \"mock\"\ntools:\n  pins:\n    message_send: \"abc123\"\n")

	cfg, err := config.Resolve(strictAgentFlags(t))
	require.NoError(t, err, "a file whose every key a setting reads passes the strict check")
	assert.Equal(t, "127.0.0.1:9000", cfg.Addr)
}

func TestResolveConfig_StrictConfigIsNotReadFromTheFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeAgentConfig(t, dir, "strict-config: true\nnonsense-key: 42\n")
	buf, restore := captureSlog(t)
	defer restore()

	_, err := config.Resolve(agentFlags(t))
	require.NoError(t, err, "strict start is a flag only, so the file cannot switch it on")
	assert.Contains(t, buf.String(), "key=strict-config",
		"the file's strict-config is a key no setting reads, so it is reported like any other")
}

// TestResolveConfig_RequireStoreIsReadFromTheFlagOnly holds that whether
// a store must already exist is said per start, on the command line
// (ADR 0025): a config file and an environment that both carry
// require-store leave it off, and the file's key is reported like any
// other key no setting reads. The control sets the flag over the same
// file and environment, and it is on.
func TestResolveConfig_RequireStoreIsReadFromTheFlagOnly(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeAgentConfig(t, dir, "require-store: true\n")
	t.Setenv("SHERLOCK_REQUIRE_STORE", "true")
	buf, restore := captureSlog(t)
	defer restore()

	cfg, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)
	assert.False(t, cfg.RequireStore, "the file or the environment switched the refusal on")
	assert.Contains(t, buf.String(), "key=require-store")

	fs := agentFlags(t)
	require.NoError(t, fs.Set("require-store", "true"))
	cfg, err = config.Resolve(fs)
	require.NoError(t, err)
	assert.True(t, cfg.RequireStore, "control: the flag switches it on")
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

func TestResolveConfig_FileLookupPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		xdgName  string
		flags    func(*testing.T) *pflag.FlagSet
		resolve  func(*pflag.FlagSet) (string, error)
	}{
		{"agent", "agent.yaml", "config.yaml", agentFlags, func(fs *pflag.FlagSet) (string, error) {
			cfg, err := config.Resolve(fs)
			return cfg.Addr, err
		}},
		{"workspace", "workspace.yaml", "workspace.yaml", workspaceFlags, func(fs *pflag.FlagSet) (string, error) {
			cfg, err := config.ResolveWorkspace(fs)
			return cfg.Addr, err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			fs := tt.flags(t)
			xdg := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", xdg)
			require.NoError(t, os.MkdirAll(filepath.Join(xdg, "sherlock"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(xdg, "sherlock", tt.xdgName),
				[]byte("addr: 127.0.0.1:9101\n"), 0o600))
			require.NoError(t, os.MkdirAll("configs", 0o755))
			local := filepath.Join("configs", tt.filename)
			require.NoError(t, os.WriteFile(local, []byte("addr: 127.0.0.1:9102\n"), 0o600))
			custom := filepath.Join(dir, "custom.yaml")
			require.NoError(t, os.WriteFile(custom, []byte("addr: 127.0.0.1:9103\n"), 0o600))
			require.NoError(t, fs.Set("config", custom))

			addr, err := tt.resolve(fs)
			require.NoError(t, err)
			assert.Equal(t, "127.0.0.1:9103", addr)

			require.NoError(t, os.Remove(custom))
			_, err = tt.resolve(fs)
			require.Error(t, err, "a missing explicit path must not fall back")

			require.NoError(t, fs.Set("config", ""))
			addr, err = tt.resolve(fs)
			require.NoError(t, err)
			assert.Equal(t, "127.0.0.1:9102", addr)

			require.NoError(t, os.WriteFile(local, []byte("addr: [unclosed\n"), 0o600))
			_, err = tt.resolve(fs)
			require.Error(t, err, "an invalid automatic file must not fall back")

			require.NoError(t, os.Remove(local))
			addr, err = tt.resolve(fs)
			require.NoError(t, err)
			assert.Equal(t, "127.0.0.1:9101", addr)
		})
	}
}

// TestResolveConfig_RevisionIsEchoedVerbatim holds that the top-level
// revision key is read as the opaque string the file carries, and that
// --strict-config accepts it as a key a setting reads.
func TestResolveConfig_RevisionIsEchoedVerbatim(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeAgentConfig(t, dir, "addr: \"127.0.0.1:9000\"\nrevision: \"desired/7 sha:Ab3\"\n")

	cfg, err := config.Resolve(strictAgentFlags(t))
	require.NoError(t, err)
	assert.Equal(t, "desired/7 sha:Ab3", cfg.Revision)
}

// TestResolveConfig_AbsentRevisionIsEmpty holds that a file without the
// revision key resolves to the empty revision, which means unknown. The
// control is the same file with the key, which resolves to its value.
func TestResolveConfig_AbsentRevisionIsEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeAgentConfig(t, dir, "addr: \"127.0.0.1:9000\"\n")
	cfg, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)
	assert.Empty(t, cfg.Revision)

	writeAgentConfig(t, dir, "addr: \"127.0.0.1:9000\"\nrevision: r1\n")
	cfg, err = config.Resolve(agentFlags(t))
	require.NoError(t, err)
	assert.Equal(t, "r1", cfg.Revision, "the control: the key is what sets it")
}

// TestResolveConfig_IssuersSection reads each issuer with its one key
// source, and require-signature from the file.
func TestResolveConfig_IssuersSection(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeAgentConfig(t, dir, "require-signature: true\n"+
		"issuers:\n"+
		"  - issuer: https://garam.example\n"+
		"    keys-file: /etc/sherlock/garam-keys.json\n"+
		"  - issuer: https://other.example/\n"+
		"    keys-url: https://other.example/keys\n"+
		"    keys-ca-file: /etc/sherlock/other-ca.pem\n")

	cfg, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)

	assert.Equal(t, []config.IssuerConfig{
		{Issuer: "https://garam.example", KeysFile: "/etc/sherlock/garam-keys.json"},
		{Issuer: "https://other.example/", KeysURL: "https://other.example/keys", KeysCAFile: "/etc/sherlock/other-ca.pem"},
	}, cfg.Issuers)
	assert.True(t, cfg.RequireSignature)

	strict := agentFlags(t)
	require.NoError(t, strict.Set("strict-config", "true"))
	_, err = config.Resolve(strict)
	assert.NoError(t, err, "every key of the issuer entries is read, so --strict-config starts")
}

// TestResolveConfig_RefusesAnIssuerItCannotActOn refuses each entry the
// gateway could not use. The control is the same file with the entry
// corrected, which resolves.
func TestResolveConfig_RefusesAnIssuerItCannotActOn(t *testing.T) {
	valid := "  - issuer: https://a.example\n    keys-file: /keys.json\n"
	cases := []struct {
		name, entry, want string
	}{
		{"both sources", "  - issuer: https://b.example\n    keys-file: /keys.json\n    keys-url: https://b.example/keys\n", "names both"},
		{"no source", "  - issuer: https://b.example\n", "names neither"},
		{"no issuer", "  - keys-file: /keys.json\n", "names no issuer"},
		{"duplicate issuer", valid, "twice"},
		{"plain-HTTP keys-url", "  - issuer: https://b.example\n    keys-url: http://b.example/keys\n", "not an https URL"},
		{"keys-ca-file with keys-file", "  - issuer: https://b.example\n    keys-file: /keys.json\n    keys-ca-file: /ca.pem\n", "keys-ca-file without keys-url"},
		{"keys-ca-file with no source", "  - issuer: https://b.example\n    keys-ca-file: /ca.pem\n", "names neither"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			writeAgentConfig(t, dir, "issuers:\n"+valid)
			_, err := config.Resolve(agentFlags(t))
			require.NoError(t, err, "control: the list without the entry resolves")

			writeAgentConfig(t, dir, "issuers:\n"+valid+tc.entry)
			_, err = config.Resolve(agentFlags(t))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestResolveConfig_IssuersEnvIsRefused refuses SHERLOCK_ISSUERS, as the
// pin set's variable is, because one variable cannot carry a list.
func TestResolveConfig_IssuersEnvIsRefused(t *testing.T) {
	t.Chdir(t.TempDir())
	_, err := config.Resolve(agentFlags(t))
	require.NoError(t, err, "control: no variable resolves")

	t.Setenv("SHERLOCK_ISSUERS", "https://garam.example")
	_, err = config.Resolve(agentFlags(t))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "SHERLOCK_ISSUERS")
	assert.Contains(t, err.Error(), "config file")
}

// TestResolveConfig_RequireSignatureResolvesThroughEveryLayer reads
// require-signature from the flag, the environment and the file, defaults
// it off, and refuses it with no issuer configured.
func TestResolveConfig_RequireSignatureResolvesThroughEveryLayer(t *testing.T) {
	issuers := "issuers:\n  - issuer: https://a.example\n    keys-file: /keys.json\n"
	dir := t.TempDir()
	t.Chdir(dir)
	writeAgentConfig(t, dir, issuers)
	cfg, err := config.Resolve(agentFlags(t))
	require.NoError(t, err)
	assert.False(t, cfg.RequireSignature, "off by default")

	t.Setenv("SHERLOCK_REQUIRE_SIGNATURE", "true")
	cfg, err = config.Resolve(agentFlags(t))
	require.NoError(t, err)
	assert.True(t, cfg.RequireSignature, "the environment turns it on")

	fs := agentFlags(t)
	require.NoError(t, fs.Set("require-signature", "false"))
	cfg, err = config.Resolve(fs)
	require.NoError(t, err)
	assert.False(t, cfg.RequireSignature, "the flag outranks the environment")

	writeAgentConfig(t, dir, "")
	_, err = config.Resolve(agentFlags(t))
	require.Error(t, err, "required with no issuer refuses every message")
	assert.Contains(t, err.Error(), "no issuer is configured")
}
