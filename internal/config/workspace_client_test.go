package config_test

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/config"
)

// workspaceClientFlags returns the flag set an operation subcommand
// resolves through: the root's shared flags merged into its own.
func workspaceClientFlags() *pflag.FlagSet {
	fs := pflag.NewFlagSet("read", pflag.ContinueOnError)
	config.AddSharedFlags(fs)
	config.AddWorkspaceClientFlags(fs)
	return fs
}

// TestResolveWorkspaceClient_Precedence resolves the address in the
// order ADR 0027 states: the flag, then SHERLOCK_WORKSPACE_ADDR, then
// the default.
func TestResolveWorkspaceClient_Precedence(t *testing.T) {
	tests := []struct {
		name string
		env  string
		args []string
		want string
	}{
		{name: "default", want: "127.0.0.1:8081"},
		{name: "environment over default", env: "127.0.0.1:9001", want: "127.0.0.1:9001"},
		{name: "flag over environment", env: "127.0.0.1:9001", args: []string{"--workspace-addr", "127.0.0.1:9002"}, want: "127.0.0.1:9002"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SHERLOCK_WORKSPACE_ADDR", tt.env)
			fs := workspaceClientFlags()
			require.NoError(t, fs.Parse(tt.args))

			cfg, err := config.ResolveWorkspaceClient(fs)
			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.Addr)
		})
	}
}

// TestResolveWorkspaceClient_RefusesConfigFileFlags refuses the shared
// flags that name a config file, since none is read (ADR 0027). The
// control is the same set with neither flag given, which resolves.
func TestResolveWorkspaceClient_RefusesConfigFileFlags(t *testing.T) {
	t.Setenv("SHERLOCK_WORKSPACE_ADDR", "")
	_, err := config.ResolveWorkspaceClient(workspaceClientFlags())
	require.NoError(t, err, "control: the set resolves without a config-file flag")

	for _, args := range [][]string{{"--config", "agent.yaml"}, {"--strict-config"}} {
		fs := workspaceClientFlags()
		require.NoError(t, fs.Parse(args))
		_, err := config.ResolveWorkspaceClient(fs)
		require.Error(t, err, "%v must be refused", args)
		assert.Contains(t, err.Error(), "read no config file")
	}
}
