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
	"github.com/spf13/pflag"

	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/message"
)

// newAgentCommand returns the "gagent agent" subcommand: it runs the
// agent's self (ADR 0018) — the gateway, the rolling session, memory, and
// the mouth — with every computer capability discovered from the
// configured tool servers. With --message it runs one turn against the
// same discovered tool set and exits; without it the agent serves its
// HTTP gateway until an interrupt or SIGTERM.
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
	fs.StringSlice("tool-servers", []string{defaultToolServer},
		"contract endpoints to discover computer tools from")
}

// newAgentFactory resolves the model providers, discovers the configured
// tool servers, and builds the runtime factory both agent modes share.
func newAgentFactory(ctx context.Context, cfg Config, ego string, sink message.Sink, logger *slog.Logger) (*gateway.Factory, error) {
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
	tools, err := discoverTools(ctx, cfg.ToolServers, logger)
	if err != nil {
		return nil, err
	}
	return newFactory(cfg, newModel, embedder, summarizer, ego, sink, tools)
}

// runAgentTurn builds the agent runtime with a stdout sink in place of
// the file outbox, captures the message as a user-turn memory (ADR 0012),
// and runs one turn, printing outbound messages and the final response
// to stdout.
func runAgentTurn(ctx context.Context, cfg Config, agentID, msg, ego string) error {
	// Logs go to stderr so stdout carries only the agent's output.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	factory, err := newAgentFactory(ctx, cfg, ego, stdoutSink{}, logger)
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

	factory, err := newAgentFactory(ctx, cfg, ego, nil, logger)
	if err != nil {
		return err
	}
	queue, err := gateway.NewQueue(factory, agentID, logger)
	if err != nil {
		return err
	}

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
			"memory_path", cfg.MemoryPath, "auth", "mtls")
		if err := srv.ListenAndServeTLS("", ""); !errors.Is(err, http.ErrServerClosed) {
			logger.Error("https server failed", "op", "serve", "error", err)
			return err
		}
	} else {
		logger.Info("agent listening",
			"op", "startup", "addr", cfg.Addr, "agent_id", agentID,
			"memory_path", cfg.MemoryPath,
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

// stdoutSink is the one-shot mode's message.Sink: it prints each outbound
// message to stdout instead of writing the file outbox.
type stdoutSink struct{}

// Send implements message.Sink.
func (stdoutSink) Send(_ context.Context, msg message.Message) error {
	fmt.Printf("[%s -> %s] %s\n", msg.Channel, msg.Target, msg.Content)
	return nil
}
