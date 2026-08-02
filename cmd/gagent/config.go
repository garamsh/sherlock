package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/garamsh/gagent/internal/environment"
	"github.com/garamsh/gagent/internal/gateway"
)

// Default settings, overridable by config file, environment, or flags.
//
// After ADR 0011 the platform binary no longer talks to a Docker daemon;
// the three per-container keys (image, cpu-limit, memory-limit) are
// dropped — they had no effect once the platform stopped spawning
// environment containers. ADR 0012 restores `exec-timeout` as the
// per-shell-call ceiling; CPU and memory limits are applied via
// setrlimit in the sub-process and are not configurable from the CLI.
const (
	defaultAddr = ":8080"

	defaultExecTimeout = 30 * time.Second

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

// Config is the platform's resolved settings, parsed once at the command
// boundary (docs/convention/runtime-safety.md) and passed down typed.
type Config struct {
	// Addr is the gateway's HTTP listen address.
	Addr string `mapstructure:"addr"`
	// DataRoot is the parent directory of all environment data.
	DataRoot string `mapstructure:"data-root"`
	// ExecTimeout caps one shell_exec sub-process run (ADR 0012).
	ExecTimeout time.Duration `mapstructure:"exec-timeout"`
	// Model selects the chat model provider (ADR 0005).
	Model ModelConfig `mapstructure:"model"`
	// Embedder selects the memory embedder provider (ADR 0005); the
	// hashing default keeps the platform fully offline.
	Embedder ModelConfig `mapstructure:"embedder"`
	// Tools lists the script tools registered alongside the built-ins.
	// gateway.ScriptTool is the config's unmarshal target directly: one
	// concept, one shape (docs/convention/simplicity.md).
	Tools []gateway.ScriptTool `mapstructure:"tools"`
}

// ModelConfig selects a model provider: the chat model under the model:
// section, the memory embedder under the embedder: section. The API key
// itself never appears here: APIKeyEnv names the environment variable
// holding it (ADR 0004).
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

// addConfigFlags registers the shared settings as flags on fs. viper
// resolves the effective value, so these defaults are the fallback layer.
func addConfigFlags(fs *pflag.FlagSet) {
	fs.String("config", "", "config file path (default: ./gagent.yaml, then XDG config dir)")
	fs.String("addr", defaultAddr, "HTTP listen address (gateway only)")
	fs.String("data-root", environment.DefaultRoot, "parent directory of all environment data")
	fs.Duration("exec-timeout", defaultExecTimeout, "per-call ceiling for shell_exec sub-processes (ADR 0012)")
}

// resolveConfig builds the effective Config for cmd using viper's built-in
// precedence: flags > GAGENT_* environment variables > config file >
// defaults. The config file resolves via --config <path>, then
// ./gagent.yaml, then os.UserConfigDir()/gagent/config.yaml (XDG;
// ~/.config/gagent/config.yaml on Linux). A missing file is an error only
// when --config named it explicitly.
func resolveConfig(cmd *cobra.Command) (Config, error) {
	v := viper.New()
	v.SetDefault("addr", defaultAddr)
	v.SetDefault("data-root", environment.DefaultRoot)
	v.SetDefault("exec-timeout", defaultExecTimeout)
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
	v.SetEnvPrefix("GAGENT")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()
	if err := v.BindPFlags(cmd.Flags()); err != nil {
		return Config{}, err
	}
	configFile, err := cmd.Flags().GetString("config")
	if err != nil {
		return Config{}, err
	}
	if err := readConfigFile(v, configFile); err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// readConfigFile loads the config file into v following the resolution
// order: an explicit --config path, then ./gagent.yaml, then
// os.UserConfigDir()/gagent/config.yaml (XDG; ~/.config/gagent/config.yaml
// on Linux). A missing file is an error only for the explicit flag path.
func readConfigFile(v *viper.Viper, flagPath string) error {
	if flagPath != "" {
		v.SetConfigFile(flagPath)
		return v.ReadInConfig()
	}
	v.SetConfigName("gagent")
	v.AddConfigPath(".")
	var notFound viper.ConfigFileNotFoundError
	if err := v.ReadInConfig(); !errors.As(err, &notFound) {
		return err
	}
	// The XDG default names the file config.yaml inside a gagent directory;
	// viper matches one base name per search, so it is addressed directly.
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	v.SetConfigFile(filepath.Join(configDir, "gagent", "config.yaml"))
	if err := v.ReadInConfig(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
