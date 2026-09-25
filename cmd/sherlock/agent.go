package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/garamsh/sherlock/internal/config"
	"github.com/garamsh/sherlock/internal/gateway"
	"github.com/garamsh/sherlock/internal/tool"
)

// newAgentCommand returns the "sherlock agent" subcommand: it runs the
// agent's self (ADR 0017) — the gateway, the rolling session, memory,
// the mouth, and the platform tool client over the operator's tool
// list. With --message it runs one turn against the same tool set and
// exits; without it the agent serves its HTTP gateway until an
// interrupt or SIGTERM.
func newAgentCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Run the agent: serve its gateway, or run one turn with --message",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Past flag parsing, so every error below is a runtime
			// outcome — a turn whose tool calls failed, a listener that
			// would not bind — and prints on its own rather than under
			// a usage dump. A flag parse error never reaches this line
			// and still prints usage.
			cmd.SilenceUsage = true
			cfg, err := config.Resolve(cmd.Flags())
			if err != nil {
				return err
			}
			ego, err := config.ResolveEgo(cmd.Flags())
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

// addAgentFlags registers the agent subcommand's flags on fs: the
// settings config resolves, plus the two inputs the command reads
// itself — which agent this process serves, and the one-shot message.
func addAgentFlags(fs *pflag.FlagSet) {
	config.AddAgentFlags(fs)
	fs.String("agent-id", "default", "agent ID this process serves")
	fs.String("message", "", "run one turn with this message and exit instead of serving")
}

// newAgentFactory resolves the model providers, loads the operator's
// tool manifests, and returns the runtime factory and the tool
// registry both agent modes share. The agent constructs no tool-shaped
// code at compile time (ADR 0017 §Decision, ADR 0009 §Decision); the
// script-tool registry is the lifecycle owner for tool calls.
func newAgentFactory(cfg config.Config, agentID, ego string, logger *slog.Logger) (*gateway.Factory, error) {
	newModel, err := newModelFunc(cfg.Model)
	if err != nil {
		return nil, err
	}
	embedder, err := newEmbedder(cfg)
	if err != nil {
		return nil, err
	}
	registry, err := buildToolRegistry(cfg, agentID, logger)
	if err != nil {
		return nil, err
	}
	return &gateway.Factory{
		MemoryPath: cfg.MemoryPath,
		NewModel:   newModel,
		Embedder:   embedder,
		Ego:        ego,
		Tools:      registry,
	}, nil
}

// buildToolRegistry loads the operator's tools directory through the
// script-tool registry (ADR 0009 §Decision). The directory is a resolved
// setting like every other one, so SHERLOCK_TOOLS_DIR reaches it through
// the layer that carries SHERLOCK_MEMORY_PATH rather than through a second
// read of the environment. The registry holds each manifest's SHA-256
// against the
// operator's tools.pins set and refuses a tool that drifted from its
// pin or that the set does not name; without the section it warns and
// loads unpinned. The required-tool check fails closed on a missing
// required name either way.
//
// The resolved values the shipped tools read from their own
// environment are published before the registry loads, because the
// runner hands the platform's environment to every tool it spawns
// (tool.SetSpawnEnv).
func buildToolRegistry(cfg config.Config, agentID string, logger *slog.Logger) (*tool.Registry, error) {
	if err := tool.SetSpawnEnv(cfg.WorkspaceAddr, gateway.DirFor(cfg.MemoryPath), agentID); err != nil {
		return nil, err
	}
	registry, err := tool.NewRegistry(tool.RegistryOptions{
		Logger:   logger,
		Dir:      cfg.ToolsDir,
		Required: tool.DefaultRequiredTools,
		Pins:     cfg.Tools.Pins,
	})
	if err != nil {
		return nil, fmt.Errorf("tool registry: %w", err)
	}
	return registry, nil
}

// runAgentTurn wires the agent runtime and hands one turn to the
// gateway, pointing its output at stdout: a CLI invocation prints the
// agent's reply without an outbox watcher, while the serving mode stays
// outbox-only.
func runAgentTurn(ctx context.Context, cfg config.Config, agentID, msg, ego string) error {
	// Logs go to stderr so stdout carries only the agent's output.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	factory, err := newAgentFactory(cfg, agentID, ego, logger)
	if err != nil {
		return err
	}
	return gateway.Turn{
		Factory:   factory,
		AgentID:   agentID,
		OutboxDir: gateway.DirFor(cfg.MemoryPath),
		Out:       os.Stdout,
		Logger:    logger,
	}.Run(ctx, msg)
}

// serveAgent runs the agent's HTTP gateway until an interrupt or SIGTERM.
// Callers authenticate with mTLS when the tls: config section is set
// (ADR 0012); without it the gateway serves loopback binds only.
func serveAgent(ctx context.Context, cfg config.Config, agentID, ego string) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	tlsConfig, err := config.ServerTLS(cfg.TLS)
	if err != nil {
		return err
	}
	if tlsConfig == nil && !config.IsLoopbackAddr(cfg.Addr) {
		return fmt.Errorf("addr %q is not a loopback bind: serving beyond localhost requires the tls: config section (ADR 0012)", cfg.Addr)
	}

	factory, err := newAgentFactory(cfg, agentID, ego, logger)
	if err != nil {
		return err
	}
	queue, err := gateway.NewQueue(factory, agentID, logger)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("agent listening",
		"op", "startup", "addr", cfg.Addr, "agent_id", agentID,
		"memory_path", cfg.MemoryPath, "workspace_addr", cfg.WorkspaceAddr,
		"tools_dir", cfg.ToolsDir,
		"auth", authLabel(tlsConfig))
	return gateway.Serve(ctx, cfg.Addr, tlsConfig, queue, logger)
}

// authLabel names the gateway's caller authentication for the startup
// log: mTLS when the tls: section built a config, otherwise the warning
// that the listener has none and is therefore loopback-only (ADR 0012).
func authLabel(tlsConfig *tls.Config) string {
	if tlsConfig != nil {
		return "mtls"
	}
	return "none: loopback binds only"
}
