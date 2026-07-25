package main

import (
	"errors"
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
}

// addConfigFlags registers the shared settings as flags on fs. viper
// resolves the effective value, so these defaults are the fallback layer.
func addConfigFlags(fs *pflag.FlagSet) {
	fs.String("addr", defaultAddr, "HTTP listen address (gateway only)")
	fs.String("data-root", environment.DefaultRoot, "parent directory of all environment data")
	fs.String("image", defaultImage, "environment container image")
	fs.Duration("exec-timeout", defaultExecTimeout, "timeout for one sandbox_exec command")
}

// resolveConfig builds the effective Config for cmd using viper's built-in
// precedence: flags > GAGENT_* environment variables > gagent.yaml >
// defaults. The config file is searched in the working directory, then
// ~/.gagent/; a missing file is not an error.
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
	v.SetEnvPrefix("GAGENT")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()
	if err := v.BindPFlags(cmd.Flags()); err != nil {
		return Config{}, err
	}
	v.SetConfigName("gagent")
	v.AddConfigPath(".")
	if home, err := os.UserHomeDir(); err == nil {
		v.AddConfigPath(filepath.Join(home, ".gagent"))
	}
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return Config{}, err
		}
	}
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
