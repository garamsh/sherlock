package config

import (
	"errors"
	"fmt"

	"github.com/spf13/pflag"
)

// workspaceAddrKey is the listener address the workspace operation
// subcommands call. It is the key the agent resolves for the same
// address, so its environment form is SHERLOCK_WORKSPACE_ADDR, the
// variable every spawned tool already holds.
const workspaceAddrKey = "workspace-addr"

// AddWorkspaceClientFlags registers the workspace operation subcommands'
// one setting as a flag on fs (ADR 0027).
func AddWorkspaceClientFlags(fs *pflag.FlagSet) {
	fs.String(workspaceAddrKey, defaultWorkspaceAddr, "gRPC address of the workspace listener to call")
}

// WorkspaceClientConfig is the workspace operation subcommands' resolved
// settings: the address of the listener they call, and nothing else.
type WorkspaceClientConfig struct {
	// Addr is the workspace listener's address.
	Addr string
}

// ResolveWorkspaceClient builds the operation subcommands' settings from
// fs: the flag, then SHERLOCK_WORKSPACE_ADDR, then the default. No config
// file is read, so the shared flags that name one are refused rather
// than ignored (ADR 0027).
func ResolveWorkspaceClient(fs *pflag.FlagSet) (WorkspaceClientConfig, error) {
	for _, name := range []string{"config", strictConfigFlag} {
		if flag := fs.Lookup(name); flag != nil && flag.Changed {
			return WorkspaceClientConfig{}, fmt.Errorf("--%s: the workspace operations read no config file; give the address with --%s or SHERLOCK_WORKSPACE_ADDR", name, workspaceAddrKey)
		}
	}
	v := newConfigViper()
	v.SetDefault(workspaceAddrKey, defaultWorkspaceAddr)
	if err := v.BindPFlag(workspaceAddrKey, fs.Lookup(workspaceAddrKey)); err != nil {
		return WorkspaceClientConfig{}, err
	}
	addr := v.GetString(workspaceAddrKey)
	if addr == "" {
		return WorkspaceClientConfig{}, errors.New("--workspace-addr is empty: the operation needs the workspace listener's address")
	}
	return WorkspaceClientConfig{Addr: addr}, nil
}
