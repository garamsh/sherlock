package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/garamsh/gagent/internal/files"
	"github.com/garamsh/gagent/internal/workspace"
	"google.golang.org/grpc"
)

// Workspace defaults, overridable by config file, environment, or flags.
const (
	// defaultWorkspace is the workspace's default directory; a pod
	// deployment mounts the workspace volume there.
	defaultWorkspace = "data/workspace"
)

// WorkspaceConfig is the workspace's resolved settings, parsed once at
// the command boundary (docs/convention/runtime-safety.md) and passed
// down typed. The workspace is the agent's data plane (ADR 0018): it
// owns the workspace directory and serves the filesystem-only gRPC
// contract — Read, Write, List — and nothing else. No tools, no exec,
// no plugin loader.
type WorkspaceConfig struct {
	// Addr is the gRPC workspace listener's address.
	Addr string `mapstructure:"addr"`
	// Workspace is the workspace directory, created on first use.
	Workspace string `mapstructure:"workspace"`
}

// newWorkspaceCommand returns the "gagent workspace" subcommand: it
// serves the workspace's filesystem-only gRPC contract (ADR 0018). The
// local addr flag shadows the shared one — the workspace's default
// listener is 8081, not the gateway's 8080.
func newWorkspaceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Serve the agent's workspace over the filesystem-only gRPC contract",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := resolveWorkspaceConfig(cmd)
			if err != nil {
				return err
			}
			return serveWorkspace(cmd.Context(), cfg)
		},
	}
	cmd.Flags().String("addr", defaultWorkspaceAddr, "gRPC listen address for the workspace contract")
	cmd.Flags().String("workspace", defaultWorkspace, "workspace directory (created on first use)")
	return cmd
}

// resolveWorkspaceConfig builds the effective WorkspaceConfig for cmd
// with the same precedence as the platform config: flags > GAGENT_*
// environment variables > config file > defaults. The workspace's
// config file resolves via --config <path>, then ./workspace.yaml,
// then os.UserConfigDir()/gagent/workspace.yaml (XDG).
func resolveWorkspaceConfig(cmd *cobra.Command) (WorkspaceConfig, error) {
	v := newConfigViper()
	v.SetDefault("addr", defaultWorkspaceAddr)
	v.SetDefault("workspace", defaultWorkspace)
	if err := v.BindPFlags(cmd.Flags()); err != nil {
		return WorkspaceConfig{}, err
	}
	configFile, err := cmd.Flags().GetString("config")
	if err != nil {
		return WorkspaceConfig{}, err
	}
	if err := readConfigFile(v, configFile, "workspace", "workspace.yaml"); err != nil {
		return WorkspaceConfig{}, err
	}
	var cfg WorkspaceConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return WorkspaceConfig{}, err
	}
	return cfg, nil
}

// serveWorkspace runs the workspace's gRPC listener until an interrupt
// or SIGTERM.
//
// The listener is unauthenticated in this phase, so it serves loopback
// binds only — the same fail-closed posture as the gateway (ADR 0013):
// an unauthenticated listener is a remote shell into the workspace,
// and a non-loopback addr fails startup.
//
// The workspace is the data plane (ADR 0018): it owns the workspace
// directory and serves Read/Write/List over gRPC. It hosts no tools,
// no process executor, and no plugin loader — those moved to the
// agent. The workspace chowns its root to the same UID the
// files-tool writes drop to, so files created through the gRPC
// contract and through agent-influenced execution share one owner.
func serveWorkspace(ctx context.Context, cfg WorkspaceConfig) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if !isLoopbackAddr(cfg.Addr) {
		return fmt.Errorf("addr %q is not a loopback bind: the gRPC listener is unauthenticated and serves loopback only until contract authentication exists (docs/architecture/security.md)", cfg.Addr)
	}

	workspaceFS, err := files.Open(cfg.Workspace, 0)
	if err != nil {
		return err
	}
	if err := files.SecureWorkspace(workspaceFS.Root(), os.Geteuid()); err != nil {
		return err
	}

	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer()
	workspace.RegisterWorkspaceServer(grpcServer, &workspace.Server{FS: workspaceFS})

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
	}()

	logger.Info("workspace listening",
		"op", "startup", "addr", cfg.Addr, "workspace", workspaceFS.Root(),
		"warning", "no authentication: pod-local listener")
	if err := grpcServer.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		logger.Error("workspace server failed", "op", "serve", "error", err)
		return err
	}
	return nil
}
