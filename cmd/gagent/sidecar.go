package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
)

// Sidecar defaults, overridable by config file, environment, or flags.
const (
	// defaultSidecarAddr binds loopback only: the contract listener is
	// pod-local and carries no mTLS in this phase (see the agent↔sidecar
	// authentication open question in docs/architecture/security.md).
	defaultSidecarAddr = "127.0.0.1:8081"

	// defaultSidecarWorkspace is the sidecar's default workspace
	// directory; a pod deployment mounts the workspace volume there.
	defaultSidecarWorkspace = "data/workspace"

	// defaultExecTimeout caps one shell_exec sub-process run.
	defaultExecTimeout = 30 * time.Second
)

// SidecarConfig is the sidecar's resolved settings, parsed once at the
// command boundary (docs/convention/runtime-safety.md) and passed down
// typed. The sidecar is the agent's computer (ADR 0018): it owns the
// workspace, the shell executor, and the script tools its config
// declares, and it knows nothing about agents, sessions, memory, or the
// gateway.
type SidecarConfig struct {
	// Addr is the contract listener's HTTP address.
	Addr string `mapstructure:"addr"`
	// Workspace is the workspace directory, created on first use.
	Workspace string `mapstructure:"workspace"`
	// ExecTimeout caps one shell_exec sub-process run.
	ExecTimeout time.Duration `mapstructure:"exec-timeout"`
	// Tools lists the script tools the sidecar serves alongside files
	// and shell_exec.
	Tools []script.Config `mapstructure:"tools"`
}

// newSidecarCommand returns the "gagent sidecar" subcommand: it serves
// the workspace, shell_exec, and the configured script tools over the
// tool wire protocol (ADR 0018). The local addr flag shadows the shared
// one — the sidecar's default listener is 8081, not the gateway's 8080.
func newSidecarCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sidecar",
		Short: "Serve the agent's computer over the tool contract",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := resolveSidecarConfig(cmd)
			if err != nil {
				return err
			}
			return serveSidecar(cmd.Context(), cfg)
		},
	}
	cmd.Flags().String("addr", defaultSidecarAddr, "HTTP listen address for the tool contract")
	cmd.Flags().String("workspace", defaultSidecarWorkspace, "workspace directory (created on first use)")
	cmd.Flags().Duration("exec-timeout", defaultExecTimeout, "per-call ceiling for shell_exec sub-processes")
	return cmd
}

// resolveSidecarConfig builds the effective SidecarConfig for cmd with
// the same precedence as the platform config: flags > GAGENT_*
// environment variables > config file > defaults. The sidecar's config
// file resolves via --config <path>, then ./sidecar.yaml, then
// os.UserConfigDir()/gagent/sidecar.yaml (XDG).
func resolveSidecarConfig(cmd *cobra.Command) (SidecarConfig, error) {
	v := newConfigViper()
	v.SetDefault("addr", defaultSidecarAddr)
	v.SetDefault("workspace", defaultSidecarWorkspace)
	v.SetDefault("exec-timeout", defaultExecTimeout)
	if err := v.BindPFlags(cmd.Flags()); err != nil {
		return SidecarConfig{}, err
	}
	configFile, err := cmd.Flags().GetString("config")
	if err != nil {
		return SidecarConfig{}, err
	}
	if err := readConfigFile(v, configFile, "sidecar", "sidecar.yaml"); err != nil {
		return SidecarConfig{}, err
	}
	var cfg SidecarConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return SidecarConfig{}, err
	}
	return cfg, nil
}

// serveSidecar runs the contract listener until an interrupt or SIGTERM.
//
// The listener is unauthenticated in this phase, so it serves loopback
// binds only — the same fail-closed posture as the gateway (ADR 0013):
// an unauthenticated contract listener is a remote shell into the
// computer, and a non-loopback addr fails startup.
//
// The tools: section is validated before anything is served, so an
// invalid entry fails startup with an error naming the tool (ADR 0009).
// The workspace is chowned to the agent UID when privileged, and every
// child process drops to it; an unprivileged sidecar reports agent_uid
// unavailable instead of silently upgrading (ADR 0008, ADR 0010).
func serveSidecar(ctx context.Context, cfg SidecarConfig) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if !isLoopbackAddr(cfg.Addr) {
		return fmt.Errorf("addr %q is not a loopback bind: the contract listener is unauthenticated and serves loopback only until contract authentication exists (docs/architecture/security.md)", cfg.Addr)
	}

	if err := script.ValidateConfigs(cfg.Tools); err != nil {
		return err
	}
	workspace, err := files.Open(cfg.Workspace, shell.DefaultAgentUID)
	if err != nil {
		return err
	}
	if err := files.SecureWorkspace(workspace.Root(), shell.DefaultAgentUID); err != nil {
		return err
	}

	executor := shell.NewProcessExecutor(shell.DefaultAgentUID, cfg.ExecTimeout)
	registry := tool.NewRegistry()
	tools := []tool.Tool{
		files.NewTool(workspace),
		shell.NewTool(executor).
			WithDefaultTimeout(cfg.ExecTimeout).
			WithWorkdir(workspace.Root()),
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

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           tool.NewHandler(registry),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("http shutdown failed", "op", "shutdown", "error", err)
		}
	}()

	logger.Info("sidecar listening",
		"op", "startup", "addr", cfg.Addr, "workspace", workspace.Root(),
		"tools", len(registry.List()),
		"warning", "no authentication: pod-local listener")
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		logger.Error("http server failed", "op", "serve", "error", err)
		return err
	}
	return nil
}
