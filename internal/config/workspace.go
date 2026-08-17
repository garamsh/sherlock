package config

import (
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/pflag"
)

// Workspace defaults, overridable by config file, environment, or flags.
const (
	// defaultWorkspace is the workspace's default directory; a pod
	// deployment mounts the workspace volume there.
	defaultWorkspace = "data/workspace"

	// defaultExecUID is the account the workspace's exec children drop
	// to (ADR 0008); it mirrors shell.DefaultAgentUID.
	defaultExecUID = 1001

	// defaultExecTimeout is the per-call ceiling for Exec commands.
	defaultExecTimeout = 30 * time.Second
)

// AddWorkspaceFlags registers the workspace's settings as flags on fs.
// The addr flag is the same name the agent registers on its own set,
// not a shadow of a shared one — only --config is shared — and its
// default listener is 8081, not the gateway's 8080.
func AddWorkspaceFlags(fs *pflag.FlagSet) {
	fs.String("addr", defaultWorkspaceAddr, "gRPC listen address for the workspace contract")
	fs.String("workspace", defaultWorkspace, "workspace directory (created on first use)")
	fs.Int("exec-uid", defaultExecUID,
		"UID exec children run as (single-account mode: must equal the workspace's own UID)")
	fs.Duration("exec-timeout", defaultExecTimeout,
		"per-call timeout ceiling for Exec commands")
}

// WorkspaceConfig is the workspace's resolved settings, parsed once at
// the command boundary (docs/convention/runtime-safety.md) and passed
// down typed. The workspace is the agent's data plane (ADR 0017): it
// owns the workspace directory and serves the gRPC contract — Read,
// Write, List, Exec — and nothing else. No tools, no plugin loader.
type WorkspaceConfig struct {
	// Addr is the gRPC workspace listener's address.
	Addr string `mapstructure:"addr"`
	// Workspace is the workspace directory, created on first use.
	Workspace string `mapstructure:"workspace"`
	// ExecUID is the UID the workspace's exec children run as
	// (ADR 0008): a privileged workspace drops children to it and
	// chowns the workspace directory to it, so files written through
	// the gRPC contract and by exec'd children share one owner; an
	// unprivileged workspace runs children only when it equals its own
	// UID (single-account mode) and otherwise fails closed with
	// agent_uid unavailable.
	ExecUID int `mapstructure:"exec-uid"`
	// ExecTimeout is the per-call ceiling for Exec commands.
	ExecTimeout time.Duration `mapstructure:"exec-timeout"`
}

// ResolveWorkspace builds the workspace's effective WorkspaceConfig
// from fs with the same precedence as the agent's: flags > GAGENT_*
// environment variables > config file > defaults. The workspace's
// config file resolves via --config <path>, then ./workspace.yaml,
// then os.UserConfigDir()/gagent/workspace.yaml (XDG).
func ResolveWorkspace(fs *pflag.FlagSet) (WorkspaceConfig, error) {
	v := newConfigViper()
	v.SetDefault("addr", defaultWorkspaceAddr)
	v.SetDefault("workspace", defaultWorkspace)
	v.SetDefault("exec-uid", defaultExecUID)
	v.SetDefault("exec-timeout", defaultExecTimeout)
	if err := v.BindPFlags(fs); err != nil {
		return WorkspaceConfig{}, err
	}
	configFile, err := fs.GetString("config")
	if err != nil {
		return WorkspaceConfig{}, err
	}
	loaded, err := readConfigFile(v, configFile, "workspace", "workspace.yaml")
	if err != nil {
		return WorkspaceConfig{}, err
	}
	var cfg WorkspaceConfig
	var used mapstructure.Metadata
	if err := v.Unmarshal(&cfg, recordKeys(&used)); err != nil {
		return WorkspaceConfig{}, err
	}
	reportUnknownKeys(loaded, used.Unused)
	return cfg, nil
}
