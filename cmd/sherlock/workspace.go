package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/garamsh/sherlock/internal/config"
	"github.com/garamsh/sherlock/internal/workspace/server"
)

// newWorkspaceCommand returns the "sherlock workspace" subcommand: it
// serves the workspace's gRPC contract (ADR 0017) — the filesystem and
// process execution. Its addr flag is its own, not the agent's: sibling
// subcommands register the same name on separate flag sets, and the
// workspace's default listener is 8081, not the gateway's 8080. Its
// subcommands are the operations that call the contract (ADR 0027) and
// the MCP server that runs them (ADR 0028); it takes no positional
// argument, so a mistyped one refuses instead of serving.
func newWorkspaceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Serve the agent's workspace over the gRPC contract (files and exec), or call it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.ResolveWorkspace(cmd.Flags())
			if err != nil {
				return err
			}
			return serveWorkspace(cmd.Context(), cfg)
		},
	}
	config.AddWorkspaceFlags(cmd.Flags())
	cmd.AddCommand(newWorkspaceOperationCommands()...)
	cmd.AddCommand(newWorkspaceMCPCommand())
	return cmd
}

// serveWorkspace resolves the startup decisions the entry point owns —
// where logs go, and whether this address may be served at all — and
// hands the data plane the rest.
//
// The listener is unauthenticated in this phase, so it serves loopback
// binds only, the same fail-closed posture as the gateway (ADR 0012):
// an unauthenticated listener is a remote shell into the workspace, and
// a non-loopback addr fails startup.
func serveWorkspace(ctx context.Context, cfg config.WorkspaceConfig) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if !config.IsLoopbackAddr(cfg.Addr) {
		return fmt.Errorf("addr %q is not a loopback bind: the gRPC listener is unauthenticated and serves loopback only until contract authentication exists (docs/architecture/security.md)", cfg.Addr)
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	return server.Serve(ctx, server.ServeOptions{
		Addr:        cfg.Addr,
		Dir:         cfg.Workspace,
		ExecUID:     cfg.ExecUID,
		ExecTimeout: cfg.ExecTimeout,
	}, logger)
}
