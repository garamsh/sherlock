package main

import (
	"context"
	"encoding/json"
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

	"github.com/garamsh/gagent/internal/agent/openai"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/message"
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

// newAgentFactory resolves the model providers, loads the operator's
// tool manifests, and returns the runtime factory and the tool
// registry both agent modes share. The agent constructs no tool-shaped
// code at compile time (ADR 0018 §Decision, ADR 0009 §Decision); the
// script-tool registry (Phase 2) is the lifecycle owner for tool calls.
func newAgentFactory(ctx context.Context, cfg Config, ego string, logger *slog.Logger) (*gateway.Factory, *tool.Registry, error) {
	newModel, err := newModelFunc(cfg.Model)
	if err != nil {
		return nil, nil, err
	}
	embedder, err := newEmbedderFunc(cfg.Embedder)
	if err != nil {
		return nil, nil, err
	}
	summarizer, err := newSummarizerFunc(cfg.Model)
	if err != nil {
		return nil, nil, err
	}
	registry, err := buildToolRegistry(cfg, logger)
	if err != nil {
		return nil, nil, err
	}
	factory, err := newFactory(cfg, newModel, embedder, summarizer, ego, registry)
	if err != nil {
		return nil, nil, err
	}
	return factory, registry, nil
}

// buildToolRegistry loads the operator's tools directory through the
// script-tool registry (ADR 0009 §Decision). The directory defaults to
// ./tools; an operator can override it through the env or the tools-dir
// config key. Each tool's manifest pins a SHA-256 of the canonical
// bytes the registry refuses to load on drift; the registry's
// required-tool check fails closed on a missing required name.
//
// The script-tool runner inherits the agent's environment when it
// spawns each tool (internal/tool/runner.go). The agent's tools read
// these variables from their own env (per-tool contracts: github
// reads GH_TOKEN, files/shell_exec dial GAGENT_WORKSPACE_ADDR,
// message_send writes to GAGENT_OUTBOX_DIR). The agent sets them
// here so the runtime's spawn inherits the resolved values; failure
// to set any required key is the operator's misconfiguration, not a
// tool startup error.
func buildToolRegistry(cfg Config, logger *slog.Logger) (*tool.Registry, error) {
	dir := toolDir(cfg)
	if err := setToolEnv(cfg); err != nil {
		return nil, err
	}
	registry, err := tool.NewRegistry(tool.RegistryOptions{
		Logger:   logger,
		Dir:      dir,
		Required: tool.DefaultRequiredTools,
		// The runtime validates inputs and outputs against the
		// sanitizer's view, so the model-facing surface and the
		// runtime's gate are the same shape (issue #319).
		Sanitize: openai.SanitizeToolParameters,
	})
	if err != nil {
		return nil, fmt.Errorf("tool registry: %w", err)
	}
	return registry, nil
}

// setToolEnv wires the operator's resolved config into the env the
// script-tool runner inherits on every spawn (ADR 0009 §Decision:
// "the runner inherits the agent's env"). GAGENT_WORKSPACE_ADDR and
// GAGENT_OUTBOX_DIR come from the config; GH_TOKEN must already be
// present in the agent's env because the agent does not materialize
// secrets (ADR 0003, ADR 0014).
func setToolEnv(cfg Config) error {
	if err := os.Setenv("GAGENT_WORKSPACE_ADDR", cfg.WorkspaceAddr); err != nil {
		return fmt.Errorf("set GAGENT_WORKSPACE_ADDR: %w", err)
	}
	outbox := messageSendOutboxDir(cfg.MemoryPath)
	if err := os.Setenv("GAGENT_OUTBOX_DIR", outbox); err != nil {
		return fmt.Errorf("set GAGENT_OUTBOX_DIR: %w", err)
	}
	return nil
}

// toolDir is the directory the registry reads manifests from. The
// default is ./tools (the in-tree location `make build-tools` populates
// in place); an operator can override via the GAGENT_TOOLS_DIR env or a
// future config key.
func toolDir(cfg Config) string {
	if dir := os.Getenv("GAGENT_TOOLS_DIR"); dir != "" {
		return dir
	}
	return tool.DefaultToolsDir
}

// messageSendOutboxDir returns the directory the message_send manifest
// writes to: the parent's outbox subdirectory, mirroring the gateway's
// outbox sink shape (docs/architecture/gateway.md).
func messageSendOutboxDir(memoryPath string) string {
	return filepath.Join(filepath.Dir(memoryPath), "outbox")
}

// runAgentTurn builds the agent runtime, captures the message as a
// user-turn memory (ADR 0012), and runs one turn. One-shot mode echoes
// outbound messages to stdout (in addition to the file outbox the
// message_send manifest runs writes to) so a CLI invocation prints the
// agent's reply without an outbox watcher. The serving mode stays
// outbox-only.
func runAgentTurn(ctx context.Context, cfg Config, agentID, msg, ego string) error {
	// Logs go to stderr so stdout carries only the agent's output.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	outboxDir := messageSendOutboxDir(cfg.MemoryPath)
	before := outboxSnapshot(outboxDir)

	factory, registry, err := newAgentFactory(ctx, cfg, ego, logger)
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
		if err := registry.Close(); err != nil {
			logger.Error("tool registry close failed", "op", "shutdown", "error", err)
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
	echoOutboxToStdout(outboxDir, before)
	return nil
}

// outboxSnapshot returns the names of the files currently in the
// outbox; the one-shot runner uses it to know which new messages the
// turn produced.
func outboxSnapshot(dir string) map[string]struct{} {
	out := map[string]struct{}{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		out[e.Name()] = struct{}{}
	}
	return out
}

// echoOutboxToStdout prints any outbox messages written during this
// turn, in name order so the run replays the delivery order. The
// file outbox is the durable record; stdout is the CLI echo.
func echoOutboxToStdout(dir string, before map[string]struct{}) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if _, seen := before[e.Name()]; seen {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var msg message.Message
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}
		fmt.Printf("[%s -> %s] %s\n", msg.Channel, msg.Target, msg.Content)
	}
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

	factory, registry, err := newAgentFactory(ctx, cfg, ego, logger)
	if err != nil {
		return err
	}
	queue, err := gateway.NewQueue(factory, agentID, logger)
	if err != nil {
		return err
	}
	defer func() {
		if err := registry.Close(); err != nil {
			logger.Error("tool registry close failed", "op", "shutdown", "error", err)
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
			"tools_dir", toolDir(cfg),
			"auth", "mtls")
		if err := srv.ListenAndServeTLS("", ""); !errors.Is(err, http.ErrServerClosed) {
			logger.Error("https server failed", "op", "serve", "error", err)
			return err
		}
	} else {
		logger.Info("agent listening",
			"op", "startup", "addr", cfg.Addr, "agent_id", agentID,
			"memory_path", cfg.MemoryPath, "workspace_addr", cfg.WorkspaceAddr,
			"tools_dir", toolDir(cfg),
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
