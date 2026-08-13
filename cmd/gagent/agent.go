package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/tool"
)

// newAgentCommand returns the "gagent agent" subcommand: it runs the
// agent's self (ADR 0018) — the gateway, the rolling session, memory,
// the mouth, and the platform tool client over the operator's tool
// list. With --message it runs one turn against the same tool set and
// exits; without it the agent serves its HTTP gateway until an
// interrupt or SIGTERM.
func newAgentCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Run the agent: serve its gateway, or run one turn with --message",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := resolveConfig(cmd)
			if err != nil {
				return err
			}
			ego, err := resolveEgo(cmd)
			if err != nil {
				return err
			}
			agentID, err := cmd.Flags().GetString("agent-id")
			if err != nil {
				return err
			}
			msg, err := cmd.Flags().GetString("message")
			if err != nil {
				return err
			}
			if msg != "" {
				return runAgentTurn(cmd.Context(), cfg, agentID, msg, ego)
			}
			return serveAgent(cmd.Context(), cfg, agentID, ego)
		},
	}
	addAgentFlags(cmd.Flags())
	return cmd
}

// addAgentFlags registers the agent subcommand's settings as flags on fs.
func addAgentFlags(fs *pflag.FlagSet) {
	fs.String("agent-id", "default", "agent ID this process serves")
	fs.String("message", "", "run one turn with this message and exit instead of serving")
	fs.String("addr", defaultAddr, "HTTP listen address")
	fs.String("memory-path", defaultMemoryPath, "SQLite memory file (created on first use)")
	fs.String("workspace-addr", defaultWorkspaceAddr,
		"gRPC endpoint of the agent's workspace (the data plane the tools call)")
	fs.String("ego-file", "",
		"ego file for the agent's instructions (default: embedded EGO.md)")
}

// newAgentFactory resolves the model providers, builds the operator's
// tool specs into a protocol client, and returns the runtime factory
// both agent modes share. The agent constructs no tool-shaped code at
// compile time (ADR 0018 §Decision, ADR 0009 §Decision).
func newAgentFactory(ctx context.Context, cfg Config, ego string, logger *slog.Logger) (*gateway.Factory, error) {
	newModel, err := newModelFunc(cfg.Model)
	if err != nil {
		return nil, err
	}
	embedder, err := newEmbedderFunc(cfg.Embedder)
	if err != nil {
		return nil, err
	}
	summarizer, err := newSummarizerFunc(cfg.Model)
	if err != nil {
		return nil, err
	}
	tools, err := buildToolClient(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}
	return newFactory(cfg, newModel, embedder, summarizer, ego, tools)
}

// buildToolClient expands the operator's tool specs into a running
// platform tool client. Each spec is a binary the agent spawns; the
// client owns the lifecycle, the discovery handshake, and the
// per-tool boundary (ADR 0018 §Tool contract).
func buildToolClient(ctx context.Context, cfg Config, logger *slog.Logger) (*tool.Client, error) {
	specs, err := expandToolSpecs(cfg)
	if err != nil {
		return nil, err
	}
	client, err := tool.NewClient(ctx, tool.Options{
		Logger:   logger,
		Policy:   tool.AllowAll(),
		Required: tool.DefaultRequiredTools,
	}, specs)
	if err != nil {
		return nil, fmt.Errorf("tool client: %w", err)
	}
	processClosers = append(processClosers, func() error { return client.Close() })
	return client, nil
}

// expandToolSpecs turns the operator's tool config into executable
// Specs. The default config (no explicit tools: section) installs the
// two first-party tools every agent needs: message_send (the mouth)
// over the file outbox, and shell_exec over the workspace gRPC. An
// explicit tools: section can override the defaults.
func expandToolSpecs(cfg Config) ([]tool.Spec, error) {
	if len(cfg.Tools) > 0 {
		out := make([]tool.Spec, 0, len(cfg.Tools))
		for _, ts := range cfg.Tools {
			spec := tool.Spec{
				Name:    ts.Name,
				Command: ts.Command,
				Args:    append([]string(nil), ts.Args...),
				Env:     append([]string(nil), ts.Env...),
			}
			if spec.Name == "" || spec.Command == "" {
				return nil, fmt.Errorf("tool spec: name and command are required")
			}
			out = append(out, spec)
		}
		return out, nil
	}
	return defaultToolSpecs(cfg)
}

// messageSendOutboxDir returns the directory the message_send binary
// writes to: the parent's outbox subdirectory, mirroring the gateway's
// outbox sink shape (docs/architecture/gateway.md).
func messageSendOutboxDir(memoryPath string) string {
	return filepath.Join(filepath.Dir(memoryPath), "outbox")
}

// defaultToolSpecs returns the two first-party tools every agent
// needs: message_send (the mouth over the file outbox) and shell_exec
// (process exec over the workspace gRPC). The two binaries live
// beside the agent binary in the same installation directory.
func defaultToolSpecs(cfg Config) ([]tool.Spec, error) {
	binDir, err := os.Executable()
	if err != nil {
		return nil, err
	}
	binDir = filepath.Dir(binDir)
	return []tool.Spec{
		{
			Name:    "message_send",
			Command: filepath.Join(binDir, "message_send"),
			Env:     []string{"GAGENT_OUTBOX_DIR=" + messageSendOutboxDir(cfg.MemoryPath)},
		},
		{
			Name:    "shell_exec",
			Command: filepath.Join(binDir, "shell_exec"),
			Env:     []string{"GAGENT_WORKSPACE_ADDR=" + cfg.WorkspaceAddr},
		},
	}, nil
}

// processClosers are the resources that survive a single turn but
// outlive any one runtime (the tool client). They run at process exit.
var processClosers []func() error

// runAgentTurn builds the agent runtime, captures the message as a
// user-turn memory (ADR 0012), and runs one turn, printing the final
// response to stdout. The outbound message_send tool writes to the
// file outbox (the operator's configured directory); the one-shot
// runner does not echo outbound messages to stdout.
func runAgentTurn(ctx context.Context, cfg Config, agentID, msg, ego string) error {
	// Logs go to stderr so stdout carries only the agent's output.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	factory, err := newAgentFactory(ctx, cfg, ego, logger)
	if err != nil {
		return err
	}
	rt, err := factory.New(ctx, agentID, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := rt.Close(context.Background()); err != nil {
			logger.Error("runtime close failed", "op", "shutdown", "error", err)
		}
		for _, close := range processClosers {
			_ = close()
		}
	}()

	if err := rt.Capture(ctx, msg); err != nil {
		return err
	}
	response, err := rt.Session.Run(ctx, msg)
	if err != nil {
		return err
	}
	fmt.Println(response)
	return nil
}

// serveAgent runs the agent's HTTP gateway until an interrupt or SIGTERM.
// Callers authenticate with mTLS when the tls: config section is set
// (ADR 0013); without it the gateway serves loopback binds only.
func serveAgent(ctx context.Context, cfg Config, agentID, ego string) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	tlsConfig, err := serverTLSConfig(cfg.TLS)
	if err != nil {
		return err
	}
	if tlsConfig == nil && !isLoopbackAddr(cfg.Addr) {
		return fmt.Errorf("addr %q is not a loopback bind: serving beyond localhost requires the tls: config section (ADR 0013)", cfg.Addr)
	}

	factory, err := newAgentFactory(ctx, cfg, ego, logger)
	if err != nil {
		return err
	}
	queue, err := gateway.NewQueue(factory, agentID, logger)
	if err != nil {
		return err
	}
	defer func() {
		for _, close := range processClosers {
			_ = close()
		}
	}()

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           gateway.NewHandler(queue, logger),
		TLSConfig:         tlsConfig,
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

	if tlsConfig != nil {
		logger.Info("agent listening",
			"op", "startup", "addr", cfg.Addr, "agent_id", agentID,
			"memory_path", cfg.MemoryPath, "workspace_addr", cfg.WorkspaceAddr,
			"auth", "mtls")
		if err := srv.ListenAndServeTLS("", ""); !errors.Is(err, http.ErrServerClosed) {
			logger.Error("https server failed", "op", "serve", "error", err)
			return err
		}
	} else {
		logger.Info("agent listening",
			"op", "startup", "addr", cfg.Addr, "agent_id", agentID,
			"memory_path", cfg.MemoryPath, "workspace_addr", cfg.WorkspaceAddr,
			"warning", "no authentication: loopback binds only")
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", "op", "serve", "error", err)
			return err
		}
	}
	if err := queue.Close(); err != nil {
		logger.Error("queue shutdown failed", "op", "shutdown", "error", err)
		return err
	}
	return nil
}
