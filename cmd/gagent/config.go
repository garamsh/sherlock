package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/garamsh/gagent/internal/tool"
)

// Default settings, overridable by config file, environment, or flags.
const (
	// defaultAddr binds loopback only: without a tls: section the
	// gateway refuses non-loopback binds (ADR 0012).
	defaultAddr = "127.0.0.1:8080"

	// defaultMemoryPath is the agent's SQLite memory file.
	defaultMemoryPath = "data/memory.db"

	// defaultWorkspaceAddr is the gRPC endpoint of the agent's
	// workspace (ADR 0017): the agent passes it to tools as
	// GAGENT_WORKSPACE_ADDR and they dial it. Plain host:port, no
	// scheme — the workspace listener is unauthenticated in this phase.
	defaultWorkspaceAddr = "127.0.0.1:8081"

	defaultModelProvider = "mock"
	defaultModelBaseURL  = "https://api.openai.com/v1"
	defaultModelName     = "gpt-4o-mini"
	defaultModelKeyEnv   = "OPENAI_API_KEY"
	// defaultModelTimeout bounds one chat-completions request. It mirrors
	// internal/agent/openai.DefaultTimeout so the default behaviour is
	// identical whether the key is set or not.
	defaultModelTimeout = 60 * time.Second

	defaultEmbedderProvider = "hashing"
	defaultEmbedderBaseURL  = "https://api.openai.com/v1"
	defaultEmbedderModel    = "text-embedding-3-small"
	defaultEmbedderKeyEnv   = "OPENAI_API_KEY"
	// defaultEmbedderTimeout bounds one embeddings request. It mirrors
	// internal/memory/openai.DefaultTimeout so the default behaviour is
	// identical whether the key is set or not.
	defaultEmbedderTimeout = 60 * time.Second
)

// Config is the agent's resolved settings, parsed once at the command
// boundary (docs/convention/runtime-safety.md) and passed down typed.
type Config struct {
	// Addr is the gateway's HTTP listen address.
	Addr string `mapstructure:"addr"`
	// MemoryPath is the agent's SQLite memory file (ADR 0017: the
	// environment directory pairing is gone; the file is configured
	// directly).
	MemoryPath string `mapstructure:"memory-path"`
	// WorkspaceAddr is the gRPC endpoint of the agent's workspace —
	// the data plane the agent's tools call into. The agent has no
	// compile-time tool registry; the script-tool runtime reads
	// tools/<name>/manifest.json files from disk and spawns them as
	// one-shot stdio processes (ADR 0009, ADR 0017); this is the
	// agent's only workspace-related setting — the workspace owns
	// exec, so exec-uid and exec-timeout are workspace keys.
	WorkspaceAddr string `mapstructure:"workspace-addr"`
	// Model selects the chat model provider (ADR 0004).
	Model ModelConfig `mapstructure:"model"`
	// Embedder selects the memory embedder provider (ADR 0004); the
	// hashing default keeps the platform fully offline.
	Embedder ModelConfig `mapstructure:"embedder"`
	// TLS carries the gateway's mTLS material (ADR 0012). When unset,
	// the gateway serves loopback binds only.
	TLS TLSConfig `mapstructure:"tls"`
	// Console selects the console sink: with URL set, every message_send
	// is POSTed to the console endpoint over mTLS (docs/architecture/
	// gateway.md). The file outbox stays the default and the
	// delivery-failure fallback.
	Console ConsoleConfig `mapstructure:"console"`
	// Tools carries the operator's statement about the installed tool
	// set (ADR 0017 §Decision: the installation list is the operator's,
	// through config).
	Tools ToolsConfig `mapstructure:"tools"`
}

// ToolsConfig is the operator's statement about the tool tree the
// script-tool runtime reads. The directory itself stays on
// GAGENT_TOOLS_DIR; what lives here is what only the operator can say.
type ToolsConfig struct {
	// Pins is the accepted tool set: tool name to the SHA-256 of that
	// tool's canonical manifest bytes, which the agent logs for every
	// tool it loads. An absent section leaves drift pinning off; a
	// present one is enforced in full, so a tool dropped into the tree
	// after the section was written is refused until its hash joins it
	// (ADR 0009 invariant 2).
	Pins tool.ManifestPins `mapstructure:"pins"`
}

// ConsoleConfig selects the console sink. An empty URL keeps the file
// outbox as the outbound sink.
type ConsoleConfig struct {
	// URL is the console endpoint outbound messages are POSTed to.
	URL string `mapstructure:"url"`
}

// TLSConfig selects the gateway's mTLS material (ADR 0012): the server
// certificate/key pair and the client CA the gateway verifies callers
// against. All three paths are set together or the section is rejected;
// an empty section means plain HTTP on loopback binds only.
type TLSConfig struct {
	// Cert is the path to the gateway's server certificate (PEM).
	Cert string `mapstructure:"cert"`
	// Key is the path to the server certificate's private key (PEM).
	Key string `mapstructure:"key"`
	// ClientCA is the path to the CA certificate bundle (PEM) client
	// certificates are verified against.
	ClientCA string `mapstructure:"client-ca"`
}

// ModelConfig selects a model provider: the chat model under the model:
// section, the memory embedder under the embedder: section. The API key
// itself never appears here: APIKeyEnv names the environment variable
// holding it (ADR 0003).
type ModelConfig struct {
	// Provider selects the implementation: "mock" (scripted) or "openai".
	Provider string `mapstructure:"provider"`
	// BaseURL is the provider's API root; any OpenAI-compatible endpoint
	// (Ollama, vLLM, …) works through the same field.
	BaseURL string `mapstructure:"base-url"`
	// Model is the model name passed to the provider.
	Model string `mapstructure:"model"`
	// APIKeyEnv names the environment variable holding the API key.
	APIKeyEnv string `mapstructure:"api-key-env"`
	// Timeout bounds one chat-completions request; <= 0 leaves the
	// provider's default in place.
	Timeout time.Duration `mapstructure:"timeout"`
}

// addConfigFlags registers the settings shared by every subcommand as
// flags on fs. viper resolves the effective value, so these defaults are
// the fallback layer.
func addConfigFlags(fs *pflag.FlagSet) {
	fs.String("config", "", "config file path (default: ./gagent.yaml, ./workspace.yaml for the workspace, then the XDG config dir)")
}

// resolveConfig builds the effective Config for cmd using viper's built-in
// precedence: flags > GAGENT_* environment variables > config file >
// defaults. The config file resolves via --config <path>, then
// ./gagent.yaml, then os.UserConfigDir()/gagent/config.yaml (XDG;
// ~/.config/gagent/config.yaml on Linux). A missing file is an error only
// when --config named it explicitly.
func resolveConfig(cmd *cobra.Command) (Config, error) {
	v := newConfigViper()
	v.SetDefault("addr", defaultAddr)
	v.SetDefault("memory-path", defaultMemoryPath)
	v.SetDefault("workspace-addr", defaultWorkspaceAddr)
	v.SetDefault("model.provider", defaultModelProvider)
	v.SetDefault("model.base-url", defaultModelBaseURL)
	v.SetDefault("model.model", defaultModelName)
	v.SetDefault("model.api-key-env", defaultModelKeyEnv)
	v.SetDefault("model.timeout", defaultModelTimeout)
	v.SetDefault("embedder.provider", defaultEmbedderProvider)
	v.SetDefault("embedder.base-url", defaultEmbedderBaseURL)
	v.SetDefault("embedder.model", defaultEmbedderModel)
	v.SetDefault("embedder.api-key-env", defaultEmbedderKeyEnv)
	v.SetDefault("embedder.timeout", defaultEmbedderTimeout)
	// Keys with neither a flag nor a default still get an empty default,
	// so the env layer (GAGENT_TLS_CERT, …) reaches Unmarshal when the
	// key is absent from the config file (issue #228).
	v.SetDefault("tls.cert", "")
	v.SetDefault("tls.key", "")
	v.SetDefault("tls.client-ca", "")
	v.SetDefault("console.url", "")
	if err := v.BindPFlags(cmd.Flags()); err != nil {
		return Config{}, err
	}
	configFile, err := cmd.Flags().GetString("config")
	if err != nil {
		return Config{}, err
	}
	if err := readConfigFile(v, configFile, "gagent", "config.yaml"); err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// newConfigViper returns a viper with the shared resolution layer both
// entry-point families use: GAGENT_* environment variables with dashes
// and dots mapped to underscores.
func newConfigViper() *viper.Viper {
	v := viper.New()
	v.SetEnvPrefix("GAGENT")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()
	return v
}

// readConfigFile loads the config file into v following the resolution
// order: an explicit --config path, then ./<cwdName>.yaml, then
// os.UserConfigDir()/gagent/<xdgName> (XDG; ~/.config/gagent/ on Linux).
// A missing file is an error only for the explicit flag path. A loaded
// file must be owner-only (docs/architecture/security.md).
func readConfigFile(v *viper.Viper, flagPath, cwdName, xdgName string) error {
	loaded, err := loadConfigFile(v, flagPath, cwdName, xdgName)
	if err != nil {
		return err
	}
	if loaded != "" {
		if err := checkConfigFileMode(loaded); err != nil {
			return err
		}
	}
	return nil
}

// loadConfigFile reads the config file into v and returns the path it
// actually loaded, or "" when no file exists (viper's ConfigFileUsed
// reports attempted paths too, so it cannot answer this).
func loadConfigFile(v *viper.Viper, flagPath, cwdName, xdgName string) (string, error) {
	if flagPath != "" {
		v.SetConfigFile(flagPath)
		return flagPath, v.ReadInConfig()
	}
	v.SetConfigName(cwdName)
	v.AddConfigPath(".")
	var notFound viper.ConfigFileNotFoundError
	if err := v.ReadInConfig(); !errors.As(err, &notFound) {
		return v.ConfigFileUsed(), err
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", nil
	}
	path := filepath.Join(configDir, "gagent", xdgName)
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return path, nil
}

// checkConfigFileMode rejects a config file readable by group or other:
// the file is operator material (tool definitions, TLS paths) and must
// be owner-only — 0600 or 0400 pass, 0644 fails. The platform validates
// and fails instead of chowning an operator-provided file.
func checkConfigFileMode(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("config: stat config file: %w", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("config: config file %q is group- or other-accessible (mode %04o); it must be owner-only (e.g. 0600 or 0400)", path, perm)
	}
	return nil
}
