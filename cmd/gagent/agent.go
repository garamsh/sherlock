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
	"github.com/garamsh/gagent/internal/shell"
	"github.com/garamsh/gagent/internal/tool"
	"github.com/garamsh/gagent/internal/workspace/client"
	"github.com/garamsh/gagent/tools/files"
	"github.com/garamsh/gagent/tools/playwright"
	shellexec "github.com/garamsh/gagent/tools/shell_exec"
)

// newAgentCommand returns the "gagent agent" subcommand: it runs the
// agent's self (ADR 0018) — the gateway, the rolling session, memory,
// the mouth, and the tools the agent imports at compile time. With
// --message it runs one turn against the same tool set and exits;
// without it the agent serves its HTTP gateway until an interrupt or
// SIGTERM.
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
		"gRPC endpoint of the agent's workspace (the data plane the files tool calls)")
	fs.Int("exec-uid", defaultExecUID,
		"UID shell_exec children run as (single-account mode: must equal the agent's own UID)")
	fs.Duration("exec-timeout", defaultExecTimeout,
		"per-call timeout ceiling for shell_exec commands")
	fs.String("workspace-path", defaultWorkspacePath,
		"shared workspace directory shell_exec children work in")
	fs.String("ego-file", "",
		"ego file for the agent's instructions (default: embedded EGO.md)")
}

// newAgentFactory resolves the model providers, dials the workspace
// gRPC client, and builds the runtime factory both agent modes share.
// The tool set is the compile-time registry the agent imports: tools
// are agent-side Go packages (ADR 0018); the agent constructs them
// here with the workspace client the workspace-addr flag dials.
func newAgentFactory(ctx context.Context, cfg Config, ego string, sink message.Sink, logger *slog.Logger) (*gateway.Factory, error) {
	// Single-account custody (ADR 0003): when exec-uid is the agent's
	// own account, exec children share it, so the agent's /proc
	// environ would be readable to them — close that channel.
	if cfg.ExecUID == os.Geteuid() {
		if err := shell.ProtectEnviron(); err != nil {
			return nil, err
		}
	}
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
	tools, err := buildTools(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}
	return newFactory(cfg, newModel, embedder, summarizer, ego, sink, tools)
}

// buildTools dials the workspace gRPC client and constructs the
// compile-time tools the agent hosts (ADR 0018): files (the workspace
// client), playwright, and shell_exec over the agent's native executor
// (ADR 0008). The agent loop never imports internal/workspace directly
// — it goes through tools, the boundary the issue specified.
func buildTools(ctx context.Context, cfg Config, logger *slog.Logger) ([]tool.Tool, error) {
	ws, closeFn, err := client.New(ctx, cfg.WorkspaceAddr)
	if err != nil {
		return nil, fmt.Errorf("dial workspace %q: %w", cfg.WorkspaceAddr, err)
	}
	// The agent process owns the connection: register its close with
	// a release hook on the runtime once we have one. For now, log
	// on shutdown so a leaked dial surfaces during tests.
	if logger != nil {
		original := closeFn
		closeFn = func() error {
			err := original()
			if err != nil {
				logger.Warn("workspace client close failed", "error", err)
			}
			return err
		}
	}
	// Pin the closeFn for the process lifetime; the agent reuses one
	// connection across the lifetime of a turn.
	processClosers = append(processClosers, closeFn)
	executor := shell.NewProcessExecutor(cfg.ExecUID, cfg.ExecTimeout)
	return []tool.Tool{
		files.New(ws),
		playwright.New(),
		shellexec.New(executor, cfg.WorkspacePath, cfg.ExecTimeout),
	}, nil
}

// processClosers are the resources that survive a single turn but
// outlive any one runtime: the workspace gRPC connection. They run at
// process exit.
var processClosers []func() error

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

	factory, err := newAgentFactory(ctx, cfg, ego, nil, logger)
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

// stdoutSink is the one-shot mode's message.Sink: it prints each outbound
// message to stdout instead of writing the file outbox.
type stdoutSink struct{}

// Send implements message.Sink.
func (stdoutSink) Send(_ context.Context, msg message.Message) error {
	fmt.Printf("[%s -> %s] %s\n", msg.Channel, msg.Target, msg.Content)
	return nil
}
