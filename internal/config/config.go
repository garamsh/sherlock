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

	"github.com/garamsh/sherlock/internal/tool"
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
	// SHERLOCK_WORKSPACE_ADDR and they dial it. Plain host:port, no
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
	// Embedding selects the embeddings endpoint recall ranks mid episodes
	// with (ADR 0004). The mock model provider ignores it.
	Embedding EmbeddingConfig `mapstructure:"embedding"`
	// TLS carries the gateway's mTLS material (ADR 0012). When unset,
	// the gateway serves loopback binds only.
	TLS TLSConfig `mapstructure:"tls"`
	// Tools carries the operator's statement about the installed tool
	// set (ADR 0017 §Decision: the installation list is the operator's,
	// through config).
	Tools ToolsConfig `mapstructure:"tools"`
	// ToolsDir is the directory the script-tool registry reads
	// manifests from (ADR 0009). It resolves through the same four
	// layers as every other setting, so SHERLOCK_TOOLS_DIR reaches it
	// the way SHERLOCK_MEMORY_PATH reaches MemoryPath.
	ToolsDir string `mapstructure:"tools-dir"`
	// Revision names the desired configuration this file is, ego
	// included. It is opaque and set by whatever writes the file; the
	// agent echoes it on its status route and never computes one.
	// Empty means unknown.
	Revision string `mapstructure:"revision"`
	// RequireStore refuses to start on a memory store that is absent or
	// uninitialized instead of creating it (ADR 0025). It is read off the
	// flag set alone: whether a store must already exist is the launcher's
	// to say for each start, and a file or environment carrying it would
	// refuse the first start too.
	RequireStore bool `mapstructure:"-"`
	// Issuers are the signers whose signed messages the gateway accepts
	// (ADR 0026). Empty trusts no issuer. The config file is the only
	// layer that can write the list: checkIssuersEnv refuses the
	// environment form.
	Issuers []IssuerConfig `mapstructure:"issuers"`
	// RequireSignature refuses a message that carries no signature
	// (ADR 0026). Off, an unsigned message is accepted under its
	// transport principal, as before signatures existed.
	RequireSignature bool `mapstructure:"require-signature"`
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
	// they wrote an empty one. The config file is the only layer that
	// can write one: checkToolPinsEnv refuses the environment form.
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

// ModelConfig selects the chat model under the model: section. The API
// key itself never appears here: APIKeyEnv names the environment
// variable holding it (ADR 0003), or APIKeyFile the file the platform
// writes it to (ADR 0029).
type ModelConfig struct {
	// Provider selects the wire protocol the endpoint speaks, not the
	// vendor behind it: "mock" (scripted), "openai-compatible" (the
	// chat-completions protocol) or "anthropic-compatible" (the Messages
	// protocol). One value serves every endpoint speaking that protocol,
	// so the vendor is chosen by BaseURL rather than here.
	Provider string `mapstructure:"provider"`
	// BaseURL is the endpoint's API root, and the field that picks the
	// vendor: any endpoint speaking the protocol Provider names works
	// through it (MiniMax, Ollama and vLLM for the OpenAI-compatible
	// one; Anthropic for the Messages one).
	BaseURL string `mapstructure:"base-url"`
	// Model is the model name passed to the provider.
	Model string `mapstructure:"model"`
	// APIKeyEnv names the environment variable holding the API key.
	APIKeyEnv string `mapstructure:"api-key-env"`
	// APIKeyFile, when set, is the path of a file holding the API key,
	// read again for every request so a key the platform rewrites takes
	// effect without a restart. APIKeyEnv is then not read, and Resolve
	// refuses an api-key-env set beside it.
	APIKeyFile string `mapstructure:"api-key-file"`
	// Timeout bounds one chat-completions request; <= 0 leaves the
	// provider's default in place.
	Timeout time.Duration `mapstructure:"timeout"`
}

// EmbeddingConfig selects an OpenAI-compatible /embeddings endpoint under
// the embedding: section. APIKeyEnv names the environment variable holding
// the key (ADR 0003); empty sends none, as a local server needs none.
type EmbeddingConfig struct {
	BaseURL   string `mapstructure:"base-url"`
	Model     string `mapstructure:"model"`
	APIKeyEnv string `mapstructure:"api-key-env"`
}

// AddSharedFlags registers the flags every subcommand resolves through,
// on fs. The flag layer is the top of this package's precedence, so the
// flags it reads are registered here rather than at the call site: a
// resolution the caller can only reach with the right flag set is one
// the caller can get wrong.
func AddSharedFlags(fs *pflag.FlagSet) {
	fs.String("config", "", "config file path (default: ./configs/agent.yaml, ./configs/workspace.yaml for the workspace, then the XDG config dir)")
	fs.Bool(strictConfigFlag, false, "refuse to start when the config file carries a key no setting reads")
}

// strictConfigFlag turns the unknown-key warning into a startup failure.
// It is read off the flag set alone, never through viper, so neither the
// environment nor the file it checks can switch it off.
const strictConfigFlag = "strict-config"

// AddAgentFlags registers the agent's settings as flags on fs. The
// agent subcommand's own inputs — which agent to serve, the one-shot
// message — are not settings and stay with the command.
func AddAgentFlags(fs *pflag.FlagSet) {
	fs.String("addr", defaultAddr, "HTTP listen address")
	fs.String("memory-path", defaultMemoryPath, "SQLite memory file (created on first use, unless --require-store)")
	fs.String("workspace-addr", defaultWorkspaceAddr,
		"gRPC endpoint of the agent's workspace (the data plane the tools call)")
	fs.String("ego-file", "",
		"ego file for the agent's instructions (default: embedded ego.md)")
	fs.String("instructions-file", "",
		"operator instructions placed between the ego and the platform contract (default: none)")
	fs.Bool(RequireStoreFlag, false,
		"refuse to start when the memory store is absent or uninitialized instead of creating it")
	fs.Bool(RequireSignatureKey, false,
		"refuse a message that carries no signature from a configured issuer")
}

// RequireStoreFlag names the flag Config.RequireStore is read from.
const RequireStoreFlag = "require-store"

// Resolve builds the agent's effective Config from fs using viper's
// built-in precedence: flags > SHERLOCK_* environment variables > config
// file > defaults. The config file resolves via --config <path>, then
// ./configs/agent.yaml, then os.UserConfigDir()/sherlock/config.yaml (XDG;
// ~/.config/sherlock/config.yaml on Linux). A missing file is an error only
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
	v.SetDefault(modelKeyEnvKey, defaultModelKeyEnv)
	v.SetDefault("model.timeout", defaultModelTimeout)
	v.SetDefault(modelKeyFileKey, "")
	// Keys with neither a flag nor a default still get an empty default,
	// so the env layer (SHERLOCK_TLS_CERT, …) reaches Unmarshal when the
	// key is absent from the config file (issue #228).
	v.SetDefault("embedding.base-url", "")
	v.SetDefault("embedding.model", "")
	v.SetDefault("embedding.api-key-env", "")
	v.SetDefault("tls.cert", "")
	v.SetDefault("tls.key", "")
	v.SetDefault("tls.client-ca", "")
	v.SetDefault("revision", "")
	if err := v.BindPFlags(fs); err != nil {
		return Config{}, err
	}
	configFile, err := fs.GetString("config")
	if err != nil {
		return Config{}, err
	}
	loaded, err := readConfigFile(v, configFile, "agent", "config.yaml")
	if err != nil {
		return Config{}, err
	}
	if err := checkIssuersEnv(); err != nil {
		return Config{}, err
	}
	var cfg Config
	var used mapstructure.Metadata
	if err := v.Unmarshal(&cfg, recordKeys(&used)); err != nil {
		return Config{}, err
	}
	if err := reportUnknownKeys(fs, loaded, used.Unused); err != nil {
		return Config{}, err
	}
	if cfg.RequireStore, err = fs.GetBool(RequireStoreFlag); err != nil {
		return Config{}, err
	}
	if err := checkToolPinsEnv(); err != nil {
		return Config{}, err
	}
	if err := checkToolPins(v, cfg.Tools.Pins); err != nil {
		return Config{}, err
	}
	if err := checkIssuers(cfg.Issuers, cfg.RequireSignature); err != nil {
		return Config{}, err
	}
	if err := checkModelKeySource(v, cfg.Model); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// modelKeyEnvKey and modelKeyFileKey are the two config keys that name
// where the model's API key is read from, and modelKeyEnvEnv the
// environment variable modelKeyEnvKey binds to.
const (
	modelKeyEnvKey  = "model.api-key-env"
	modelKeyFileKey = "model.api-key-file"
	modelKeyEnvEnv  = "SHERLOCK_MODEL_API_KEY_ENV"
)

// checkModelKeySource refuses a model section that names both key
// sources. api-key-env has a default, so only one the operator wrote —
// in the file or through the environment — counts as named; without
// this check the file would win silently and the variable the operator
// set would never be read.
func checkModelKeySource(v *viper.Viper, model ModelConfig) error {
	if model.APIKeyFile == "" {
		return nil
	}
	if _, ok := os.LookupEnv(modelKeyEnvEnv); !ok && !v.InConfig(modelKeyEnvKey) {
		return nil
	}
	return fmt.Errorf("config: %s and %s are both set; name one source for the model's API key",
		modelKeyFileKey, modelKeyEnvKey)
}

// toolPinsKey is the config key holding the operator's accepted tool
// set. It is named here because two things read it: the resolution that
// binds it, and the check that an operator who opened the section
// filled it.
const toolPinsKey = "tools.pins"

// toolPinsEnv is the environment variable toolPinsKey binds to under the
// prefix and replacer newConfigViper sets. It is named to be refused.
const toolPinsEnv = "SHERLOCK_TOOLS_PINS"

// checkToolPinsEnv refuses the environment form of the pin set.
//
// Every key binds to the environment, so this variable makes tools.pins
// present and unmarshals its scalar to an empty map — the state
// checkToolPins rejects, which answered an operator who named two tools
// that they had named none (#745).
//
// The refusal covers an empty value too. Naming the variable is the
// belief that the environment reaches this key, and an empty one would
// otherwise leave pinning off while reading as configured, which is the
// shape #562 cost.
//
// There is no encoding to give it instead. A pin set maps each tool name
// to a manifest SHA-256, and a value a shell splits or a pod spec folds
// stays well-formed: it reaches the registry as descriptor drift, which
// is what a tampered manifest reaches it as. The config file is also the
// only layer holding the pin set's custody — it is mode-checked
// owner-only, where every tool the agent spawns inherits the whole
// environment.
func checkToolPinsEnv() error {
	if _, ok := os.LookupEnv(toolPinsEnv); !ok {
		return nil
	}
	return fmt.Errorf("config: %s is set, and the environment cannot express %s: a pin set maps each "+
		"tool name to its manifest SHA-256, and one variable carries one value. Write the pin set in "+
		"the agent's config file (--config <path>, ./configs/agent.yaml, or the XDG config path) and unset %s",
		toolPinsEnv, toolPinsKey, toolPinsEnv)
}

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
// entry-point families use: SHERLOCK_* environment variables with dashes
// and dots mapped to underscores.
func newConfigViper() *viper.Viper {
	v := viper.New()
	v.SetEnvPrefix("SHERLOCK")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()
	return v
}

// readConfigFile loads the config file into v following the resolution
// order: an explicit --config path, then ./configs/<cwdName>.yaml, then
// os.UserConfigDir()/sherlock/<xdgName> (XDG; ~/.config/sherlock/ on Linux).
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
// removed feature. By default it reports rather than refuses, because
// refusing would reject an operator's whole file the moment a key is
// renamed. Under --strict-config it refuses instead, so a process that
// started has read every key its file carries.
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
func reportUnknownKeys(fs *pflag.FlagSet, loaded string, unused []string) error {
	strict, err := fs.GetBool(strictConfigFlag)
	if err != nil {
		return err
	}
	if loaded == "" || len(unused) == 0 {
		return nil
	}
	fileKeys := configFileKeys(loaded)
	if len(fileKeys) == 0 {
		return nil
	}
	unknown := make([]string, 0, len(unused))
	for _, key := range unused {
		if carriedBy(fileKeys, key) {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	if strict && len(unknown) > 0 {
		return fmt.Errorf("config: %s carries %s, which no setting reads; --%s refuses to start on an unread key",
			loaded, strings.Join(unknown, ", "), strictConfigFlag)
	}
	for _, key := range unknown {
		slog.Warn("unknown config key: nothing reads it",
			"op", "config.load", "key", key, "file", loaded)
	}
	return nil
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
	v.AddConfigPath("./configs")
	var notFound viper.ConfigFileNotFoundError
	if err := v.ReadInConfig(); !errors.As(err, &notFound) {
		return v.ConfigFileUsed(), err
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", nil
	}
	path := filepath.Join(configDir, "sherlock", xdgName)
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return path, nil
}
