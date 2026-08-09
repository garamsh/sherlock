package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/garamsh/gagent/internal/files"
	"github.com/garamsh/gagent/internal/shell"
	"github.com/garamsh/gagent/internal/tool"
	"github.com/garamsh/gagent/internal/tool/script"
	"github.com/garamsh/gagent/internal/workspace"
	"google.golang.org/grpc"
)

// Workspace defaults, overridable by config file, environment, or flags.
const (
	// defaultWorkspaceAddr binds loopback only: the contract listener is
	// pod-local and carries no mTLS in this phase (see the agent↔workspace
	// authentication open question in docs/architecture/security.md).
	defaultWorkspaceAddr = "127.0.0.1:8081"

	// defaultWorkspace is the workspace's default directory; a pod
	// deployment mounts the workspace volume there.
	defaultWorkspace = "data/workspace"

	// defaultExecTimeout caps one shell_exec sub-process run.
	defaultExecTimeout = 30 * time.Second
)

// WorkspaceConfig is the workspace's resolved settings, parsed once at
// the command boundary (docs/convention/runtime-safety.md) and passed
// down typed. The workspace is the agent's data plane (ADR 0018): it
// owns the workspace, the shell executor, and the script tools its
// config declares, and it knows nothing about agents, sessions,
// memory, or the gateway.
type WorkspaceConfig struct {
	// Addr is the contract listener's HTTP address.
	Addr string `mapstructure:"addr"`
	// Workspace is the workspace directory, created on first use.
	Workspace string `mapstructure:"workspace"`
	// ExecTimeout caps one shell_exec sub-process run.
	ExecTimeout time.Duration `mapstructure:"exec-timeout"`
	// ExecUID is the account tool children run as (ADR 0008). When it
	// differs from the workspace's euid the privilege to switch is
	// required exactly as before; when it equals the euid the deployment
	// declares a single-account workspace and no privilege is needed.
	ExecUID int `mapstructure:"exec-uid"`
	// Tools lists the script tools the workspace serves alongside files
	// and shell_exec.
	Tools []script.Config `mapstructure:"tools"`
}

// newWorkspaceCommand returns the "gagent workspace" subcommand: it
// serves the workspace, shell_exec, and the configured script tools
// over the tool wire protocol (ADR 0018). The local addr flag shadows
// the shared one — the workspace's default listener is 8081, not the
// gateway's 8080.
func newWorkspaceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Serve the agent's workspace over the tool contract",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := resolveWorkspaceConfig(cmd)
			if err != nil {
				return err
			}
			return serveWorkspace(cmd.Context(), cfg)
		},
	}
	cmd.Flags().String("addr", defaultWorkspaceAddr, "HTTP listen address for the tool contract")
	cmd.Flags().String("workspace", defaultWorkspace, "workspace directory (created on first use)")
	cmd.Flags().Duration("exec-timeout", defaultExecTimeout, "per-call ceiling for shell_exec sub-processes")
	cmd.Flags().Int("exec-uid", shell.DefaultAgentUID,
		"account tool children run as; set to the workspace's own UID for single-account mode (no privilege required)")
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
	v.SetDefault("exec-timeout", defaultExecTimeout)
	v.SetDefault("exec-uid", shell.DefaultAgentUID)
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

// serveWorkspace runs the contract listener until an interrupt or
// SIGTERM.
//
// The listener is unauthenticated in this phase, so it serves loopback
// binds only — the same fail-closed posture as the gateway (ADR 0013):
// an unauthenticated contract listener is a remote shell into the
// workspace, and a non-loopback addr fails startup.
//
// The tools: section is validated before anything is served, so an
// invalid entry fails startup with an error naming the tool (ADR 0009).
// The workspace is chowned to exec-uid when privileged, and every child
// process drops to it; when exec-uid equals the workspace's own UID the
// run is a single-account workspace and needs no privilege, and with
// any other unprivileged exec-uid the workspace reports agent_uid
// unavailable instead of silently upgrading (ADR 0008, ADR 0010).
func serveWorkspace(ctx context.Context, cfg WorkspaceConfig) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if !isLoopbackAddr(cfg.Addr) {
		return fmt.Errorf("addr %q is not a loopback bind: the contract listener is unauthenticated and serves loopback only until contract authentication exists (docs/architecture/security.md)", cfg.Addr)
	}

	// Single-account custody (ADR 0003): when exec-uid is the workspace's
	// own account, tool children share it, so the workspace's /proc
	// environ would be readable to them — close that channel.
	if cfg.ExecUID == os.Geteuid() {
		if err := shell.ProtectEnviron(); err != nil {
			return err
		}
	}

	if err := script.ValidateConfigs(cfg.Tools); err != nil {
		return err
	}
	workspaceFS, err := files.Open(cfg.Workspace, cfg.ExecUID)
	if err != nil {
		return err
	}
	if err := files.SecureWorkspace(workspaceFS.Root(), cfg.ExecUID); err != nil {
		return err
	}

	executor := shell.NewProcessExecutor(cfg.ExecUID, cfg.ExecTimeout)
	registry := tool.NewRegistry()
	tools := []tool.Tool{
		files.NewTool(workspaceFS),
		shell.NewTool(executor).
			WithDefaultTimeout(cfg.ExecTimeout).
			WithWorkdir(workspaceFS.Root()),
	}
	for _, c := range cfg.Tools {
		spec, err := c.Spec()
		if err != nil {
			return err
		}
		tools = append(tools, script.New(spec, executor))
	}
	for _, tl := range tools {
		if err := registry.Register(tl); err != nil {
			return err
		}
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	grpcListener, err := net.Listen("tcp", "127.0.0.1:9090")
	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer()
	workspace.RegisterWorkspaceServer(grpcServer, &workspace.Server{FS: workspaceFS})
	go func() {
		if err := grpcServer.Serve(grpcListener); err != nil {
			logger.Error("workspace server failed", "error", err)
		}
	}()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           tool.NewHandler(registry),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		grpcServer.GracefulStop()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("http shutdown failed", "op", "shutdown", "error", err)
		}
	}()

	logger.Info("workspace listening",
		"op", "startup", "addr", cfg.Addr, "workspace", workspaceFS.Root(),
		"tools", len(registry.List()),
		"warning", "no authentication: pod-local listener")
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		logger.Error("http server failed", "op", "serve", "error", err)
		return err
	}
	return nil
}
