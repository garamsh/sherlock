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
	"time"

	"github.com/spf13/cobra"

	"github.com/garamsh/gagent/internal/files"
	"github.com/garamsh/gagent/internal/workspace"
	"github.com/garamsh/gagent/internal/workspace/shell"
	"google.golang.org/grpc"
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

// WorkspaceConfig is the workspace's resolved settings, parsed once at
// the command boundary (docs/convention/runtime-safety.md) and passed
// down typed. The workspace is the agent's data plane (ADR 0018): it
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

// newWorkspaceCommand returns the "gagent workspace" subcommand: it
// serves the workspace's gRPC contract (ADR 0018) — the filesystem and
// process execution. The local addr flag shadows the shared one — the
// workspace's default listener is 8081, not the gateway's 8080.
func newWorkspaceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Serve the agent's workspace over the gRPC contract (files and exec)",
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
	cmd.Flags().Int("exec-uid", defaultExecUID,
		"UID exec children run as (single-account mode: must equal the workspace's own UID)")
	cmd.Flags().Duration("exec-timeout", defaultExecTimeout,
		"per-call timeout ceiling for Exec commands")
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
	v.SetDefault("exec-uid", defaultExecUID)
	v.SetDefault("exec-timeout", defaultExecTimeout)
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
// directory, serves Read/Write/List over gRPC, and runs Exec children
// through its own executor — the agent executes nothing itself. The
// workspace chowns its root to the exec UID when privileged, so files
// written through the gRPC contract and by exec'd children share one
// owner.
func serveWorkspace(ctx context.Context, cfg WorkspaceConfig) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if !isLoopbackAddr(cfg.Addr) {
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
	if err := files.SecureWorkspace(workspaceFS.Root(), cfg.ExecUID); err != nil {
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
