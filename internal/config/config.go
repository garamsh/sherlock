package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
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
	// Tools carries the operator's statement about the installed tool
	// set (ADR 0017 §Decision: the installation list is the operator's,
	// through config).
	Tools ToolsConfig `mapstructure:"tools"`
	// ToolsDir is the directory the script-tool registry reads
	// manifests from (ADR 0009). It resolves through the same four
	// layers as every other setting, so GAGENT_TOOLS_DIR reaches it
	// the way GAGENT_MEMORY_PATH reaches MemoryPath.
	ToolsDir string `mapstructure:"tools-dir"`
}

// ToolsConfig is the operator's statement about the tool tree the
// script-tool runtime reads: what lives here is what only the operator
// can say. Where the tree is read from is ToolsDir, an ordinary setting
// on Config.
type ToolsConfig struct {
	// Pins is the accepted tool set: tool name to the SHA-256 of that
	// tool's canonical manifest bytes, which the agent logs for every
	// tool it loads. An absent section leaves drift pinning off; a
	// present one is enforced in full, so a tool dropped into the tree
	// after the section was written is refused until its hash joins it
	// (ADR 0009 invariant 2). A present section naming no tool is
	// neither, and Resolve rejects it — checkToolPins says why.
	//
	// Nil therefore means the operator wrote no section, and never that
	// they wrote an empty one.
	Pins tool.ManifestPins `mapstructure:"pins"`
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

// AddSharedFlags registers the flags every subcommand resolves through,
// on fs. The flag layer is the top of this package's precedence, so the
// flags it reads are registered here rather than at the call site: a
// resolution the caller can only reach with the right flag set is one
// the caller can get wrong.
func AddSharedFlags(fs *pflag.FlagSet) {
	fs.String("config", "", "config file path (default: ./gagent.yaml, ./workspace.yaml for the workspace, then the XDG config dir)")
}

// AddAgentFlags registers the agent's settings as flags on fs. The
// agent subcommand's own inputs — which agent to serve, the one-shot
// message — are not settings and stay with the command.
func AddAgentFlags(fs *pflag.FlagSet) {
	fs.String("addr", defaultAddr, "HTTP listen address")
	fs.String("memory-path", defaultMemoryPath, "SQLite memory file (created on first use)")
	fs.String("workspace-addr", defaultWorkspaceAddr,
		"gRPC endpoint of the agent's workspace (the data plane the tools call)")
	fs.String("ego-file", "",
		"ego file for the agent's instructions (default: embedded EGO.md)")
}

// Resolve builds the agent's effective Config from fs using viper's
// built-in precedence: flags > GAGENT_* environment variables > config
// file > defaults. The config file resolves via --config <path>, then
// ./gagent.yaml, then os.UserConfigDir()/gagent/config.yaml (XDG;
// ~/.config/gagent/config.yaml on Linux). A missing file is an error only
// when --config named it explicitly.
func Resolve(fs *pflag.FlagSet) (Config, error) {
	v := newConfigViper()
	v.SetDefault("addr", defaultAddr)
	v.SetDefault("memory-path", defaultMemoryPath)
	v.SetDefault("workspace-addr", defaultWorkspaceAddr)
	v.SetDefault("tools-dir", tool.DefaultToolsDir)
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
	if err := v.BindPFlags(fs); err != nil {
		return Config{}, err
	}
	configFile, err := fs.GetString("config")
	if err != nil {
		return Config{}, err
	}
	loaded, err := readConfigFile(v, configFile, "gagent", "config.yaml")
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	var used mapstructure.Metadata
	if err := v.Unmarshal(&cfg, recordKeys(&used)); err != nil {
		return Config{}, err
	}
	reportUnknownKeys(loaded, used.Unused)
	if err := checkToolPins(v, cfg.Tools.Pins); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// toolPinsKey is the config key holding the operator's accepted tool
// set. It is named here because two things read it: the resolution that
// binds it, and the check that an operator who opened the section
// filled it.
const toolPinsKey = "tools.pins"

// checkToolPins rejects a pin section that names no tool.
//
// The three states an operator can write are not three settings. An
// absent section declines to pin, and the registry says so at startup.
// A populated one is the tool set the operator accepted. A present but
// empty one is neither, and it cannot mean "accept no tool": message_send
// is required and the required set is not operator-configurable, so a
// registry accepting nothing fails its required check whatever the tools
// directory holds. There is no tree in which an empty set produces a
// running agent, which is why it is read as a section someone opened and
// did not fill rather than as the strictest setting.
//
// It is checked here because here is the only place the state is
// visible. viper's Unmarshal leaves an empty mapping nil, exactly as it
// leaves an absent one, so by the time a pin set reaches the registry
// the two are the same value — which is how the strictest-looking
// setting came to turn pinning off and say nothing (#562).
func checkToolPins(v *viper.Viper, pins tool.ManifestPins) error {
	if len(pins) > 0 || !v.IsSet(toolPinsKey) {
		return nil
	}
	return fmt.Errorf("config: %s is present and names no tool; a pin set is the tools the operator "+
		"accepted, so an empty one accepts nothing and no agent can start under it. Name each tool "+
		"and the manifest hash the agent logs for it, or remove the section to run unpinned", toolPinsKey)
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
// file must be owner-only (docs/architecture/security.md). It returns
// the path it loaded, or "" when no file exists.
func readConfigFile(v *viper.Viper, flagPath, cwdName, xdgName string) (string, error) {
	loaded, err := loadConfigFile(v, flagPath, cwdName, xdgName)
	if err != nil {
		return "", err
	}
	if loaded != "" {
		if err := RequireOwnerOnly("config file", loaded); err != nil {
			return "", err
		}
	}
	return loaded, nil
}

// recordKeys makes Unmarshal record which input keys it consumed, so
// the ones it did not can be reported.
func recordKeys(md *mapstructure.Metadata) viper.DecoderConfigOption {
	return func(dc *mapstructure.DecoderConfig) { dc.Metadata = md }
}

// reportUnknownKeys warns for every key the config file carries that no
// setting reads: a typo, a renamed key, or a block left over from a
// removed feature. It reports rather than refuses, because refusing
// would reject an operator's whole file the moment a key is renamed.
//
// The warning names the key and the file, which is what makes it
// actionable: `tools.pin` for `tools.pins` turns drift pinning off
// while the registry reports it as never configured (ADR 0009
// invariant 2), and only the key's own name separates the two.
//
// unused carries every input key Unmarshal left over, which includes
// the command's flags — they are bound to the same viper and most name
// no setting. Only the keys the file itself carries are the operator's
// to fix, so the file is what they are filtered against.
func reportUnknownKeys(loaded string, unused []string) {
	if loaded == "" || len(unused) == 0 {
		return
	}
	fileKeys := configFileKeys(loaded)
	if len(fileKeys) == 0 {
		return
	}
	unknown := make([]string, 0, len(unused))
	for _, key := range unused {
		if carriedBy(fileKeys, key) {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	for _, key := range unknown {
		slog.Warn("unknown config key: nothing reads it",
			"op", "config.load", "key", key, "file", loaded)
	}
}

// carriedBy reports whether the config file holds key, either as a leaf
// of its own or as the root of a section: an unread `console:` section
// is left over as `console`, while the file spells its leaf
// `console.url`.
func carriedBy(fileKeys []string, key string) bool {
	for _, fileKey := range fileKeys {
		if fileKey == key || strings.HasPrefix(fileKey, key+".") {
			return true
		}
	}
	return false
}

// configFileKeys returns the keys the config file itself carries,
// separately from the defaults, flags and environment merged alongside
// them. A file that no longer parses yields none: it was read once
// already, and a diagnostic is not the place to fail on a second read.
func configFileKeys(path string) []string {
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil
	}
	return v.AllKeys()
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
