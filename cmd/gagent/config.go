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
)

// Default settings, overridable by config file, environment, or flags.
const (
	defaultAddr        = ":8080"
	defaultImage       = "alpine:3.21"
	defaultExecTimeout = 30 * time.Second

	defaultModelProvider = "mock"
	defaultModelBaseURL  = "https://api.openai.com/v1"
	defaultModelName     = "gpt-4o-mini"
	defaultModelKeyEnv   = "OPENAI_API_KEY"
	// defaultModelTimeout bounds one chat-completions request. It mirrors
	// internal/agent/openai.DefaultTimeout so the default behaviour is
	// identical whether the key is set or not.
	defaultModelTimeout = 60 * time.Second
)

// Config is the platform's resolved settings, parsed once at the command
// boundary (docs/convention/runtime-safety.md) and passed down typed.
type Config struct {
	// Addr is the gateway's HTTP listen address.
	Addr string `mapstructure:"addr"`
	// DataRoot is the parent directory of all environment data.
	DataRoot string `mapstructure:"data-root"`
	// Image is the environment container image reference.
	Image string `mapstructure:"image"`
	// ExecTimeout bounds every sandbox_exec command.
	ExecTimeout time.Duration `mapstructure:"exec-timeout"`
	// Model selects the chat model provider (ADR 0005).
	Model ModelConfig `mapstructure:"model"`
	// MCP lists the external MCP servers whose tools join every
	// environment's registry (issue #34).
	MCP MCPConfig `mapstructure:"mcp"`
}

// MCPConfig lists the external MCP servers started at platform boot.
type MCPConfig struct {
	// Servers are the configured stdio MCP servers.
	Servers []MCPServerConfig `mapstructure:"servers"`
}

// MCPServerConfig describes one stdio MCP server subprocess.
type MCPServerConfig struct {
	// Name labels the server in logs and errors.
	Name string `mapstructure:"name"`
	// Command is the executable to run.
	Command string `mapstructure:"command"`
	// Args are the command arguments.
	Args []string `mapstructure:"args"`
	// Env allowlists platform environment variable names forwarded to the
	// server process; everything else is scrubbed (ADR 0004 spirit).
	Env []string `mapstructure:"env"`
	// Timeout bounds one tools/call; <= 0 uses mcp.DefaultCallTimeout.
	Timeout time.Duration `mapstructure:"timeout"`
}

// ModelConfig selects the chat model provider. The API key itself never
// appears here: APIKeyEnv names the environment variable holding it
// (ADR 0004).
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
	fs.String("image", defaultImage, "environment container image")
	fs.Duration("exec-timeout", defaultExecTimeout, "timeout for one sandbox_exec command")
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
	v.SetDefault("image", defaultImage)
	v.SetDefault("exec-timeout", defaultExecTimeout)
	v.SetDefault("model.provider", defaultModelProvider)
	v.SetDefault("model.base-url", defaultModelBaseURL)
	v.SetDefault("model.model", defaultModelName)
	v.SetDefault("model.api-key-env", defaultModelKeyEnv)
	v.SetDefault("model.timeout", defaultModelTimeout)
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
