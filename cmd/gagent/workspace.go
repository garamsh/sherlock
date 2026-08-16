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

	"github.com/garamsh/gagent/internal/config"
	"github.com/garamsh/gagent/internal/workspace"
	"github.com/garamsh/gagent/internal/workspace/files"
	"github.com/garamsh/gagent/internal/workspace/shell"
	"google.golang.org/grpc"
)

// newWorkspaceCommand returns the "gagent workspace" subcommand: it
// serves the workspace's gRPC contract (ADR 0017) — the filesystem and
// process execution. The local addr flag shadows the shared one — the
// workspace's default listener is 8081, not the gateway's 8080.
func newWorkspaceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Serve the agent's workspace over the gRPC contract (files and exec)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.ResolveWorkspace(cmd.Flags())
			if err != nil {
				return err
			}
			return serveWorkspace(cmd.Context(), cfg)
		},
	}
	config.AddWorkspaceFlags(cmd.Flags())
	return cmd
}

// serveWorkspace runs the workspace's gRPC listener until an interrupt
// or SIGTERM.
//
// The listener is unauthenticated in this phase, so it serves loopback
// binds only — the same fail-closed posture as the gateway (ADR 0012):
// an unauthenticated listener is a remote shell into the workspace,
// and a non-loopback addr fails startup.
//
// The workspace is the data plane (ADR 0017): it owns the workspace
// directory, serves Read/Write/List over gRPC, and runs Exec children
// through its own executor — the agent executes nothing itself. The
// workspace chowns its root to the exec UID when privileged, so files
// written through the gRPC contract and by exec'd children share one
// owner.
func serveWorkspace(ctx context.Context, cfg config.WorkspaceConfig) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if !config.IsLoopbackAddr(cfg.Addr) {
		return fmt.Errorf("addr %q is not a loopback bind: the gRPC listener is unauthenticated and serves loopback only until contract authentication exists (docs/architecture/security.md)", cfg.Addr)
	}

	// Single-account custody (ADR 0003): when exec-uid is the
	// workspace's own account, exec children share it, so the
	// workspace's /proc environ would be readable to them — close that
	// channel.
	if cfg.ExecUID == os.Geteuid() {
		if err := shell.ProtectEnviron(); err != nil {
			return err
		}
	}

	workspaceFS, err := files.Open(cfg.Workspace)
	if err != nil {
		return err
	}
	if err := workspaceFS.HandOver(cfg.ExecUID); err != nil {
		return err
	}

	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer()
	workspace.RegisterWorkspaceServer(grpcServer, &workspace.Server{
		FS:       workspaceFS,
		Executor: shell.NewProcessExecutor(cfg.ExecUID, cfg.ExecTimeout),
	})

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
