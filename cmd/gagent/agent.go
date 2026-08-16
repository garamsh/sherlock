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
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/config"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/message"
	"github.com/garamsh/gagent/internal/tool"
)

// newAgentCommand returns the "gagent agent" subcommand: it runs the
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
// script-tool registry (Phase 2) is the lifecycle owner for tool calls.
func newAgentFactory(ctx context.Context, cfg config.Config, ego string, logger *slog.Logger) (*gateway.Factory, *tool.Registry, error) {
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
// script-tool registry (ADR 0009 §Decision). The directory is a resolved
// setting like every other one, so GAGENT_TOOLS_DIR reaches it through
// the layer that carries GAGENT_MEMORY_PATH rather than through a second
// read of the environment. The registry holds each manifest's SHA-256
// against the
// operator's tools.pins set and refuses a tool that drifted from its
// pin or that the set does not name; without the section it warns and
// loads unpinned. The required-tool check fails closed on a missing
// required name either way.
//
// The script-tool runner inherits the agent's environment when it
// spawns each tool (internal/tool/runner.go). The agent's tools read
// these variables from their own env (per-tool contracts: github
// reads GH_TOKEN, files/shell_exec dial GAGENT_WORKSPACE_ADDR,
// message_send writes to GAGENT_OUTBOX_DIR). The agent sets them
// here so the runtime's spawn inherits the resolved values; failure
// to set any required key is the operator's misconfiguration, not a
// tool startup error.
func buildToolRegistry(cfg config.Config, logger *slog.Logger) (*tool.Registry, error) {
	if err := setToolEnv(cfg); err != nil {
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

// setToolEnv wires the operator's resolved config into the env the
// script-tool runner inherits on every spawn (ADR 0009 §Decision:
// "the runner inherits the agent's env"). GAGENT_WORKSPACE_ADDR and
// GAGENT_OUTBOX_DIR come from the config; GH_TOKEN must already be
// present in the agent's env because the agent does not materialize
// secrets (ADR 0003, ADR 0013).
func setToolEnv(cfg config.Config) error {
	if err := os.Setenv("GAGENT_WORKSPACE_ADDR", cfg.WorkspaceAddr); err != nil {
		return fmt.Errorf("set GAGENT_WORKSPACE_ADDR: %w", err)
	}
	outbox := messageSendOutboxDir(cfg.MemoryPath)
	if err := os.Setenv("GAGENT_OUTBOX_DIR", outbox); err != nil {
		return fmt.Errorf("set GAGENT_OUTBOX_DIR: %w", err)
	}
	return nil
}

// messageSendOutboxDir returns the directory the message_send manifest
// writes to: the parent's outbox subdirectory, mirroring the gateway's
// outbox sink shape (docs/architecture/gateway.md).
func messageSendOutboxDir(memoryPath string) string {
	return filepath.Join(filepath.Dir(memoryPath), "outbox")
}

// failedToolCalls is the one-shot run's event sink: it collects the
// names of the turn's failed tool calls so the process can report them
// in its exit status.
//
// Only the one-shot path wires this. A served turn hands the session
// the gateway's monitoring sink instead, and its failed tool call goes
// to the model, which gets another iteration to react — that is the
// loop working (docs/architecture/agent.md). A one-shot run has no such
// reader: the turn ends, the process exits, and nothing acts on the
// failure, so exiting 0 would report a clean run that did not happen.
//
// The session emits from the goroutine that called Run, and the
// one-shot path calls Run once, so the collection needs no lock.
type failedToolCalls struct {
	names []string
}

// Emit implements agent.EventSink.
func (f *failedToolCalls) Emit(_ context.Context, e agent.Event) {
	if e.Kind == agent.EventToolCall && e.ToolFailed {
		f.names = append(f.names, e.ToolName)
	}
}

// runAgentTurn builds the agent runtime and runs one turn. One-shot mode
// echoes outbound messages to stdout (in addition to the file outbox the
// message_send manifest runs writes to) so a CLI invocation prints the
// agent's reply without an outbox watcher. The serving mode stays
// outbox-only.
//
// The turn's output is printed before a failed tool call is reported,
// so a caller reading stdout sees what the agent produced either way.
func runAgentTurn(ctx context.Context, cfg config.Config, agentID, msg, ego string) error {
	// Logs go to stderr so stdout carries only the agent's output.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	outboxDir := messageSendOutboxDir(cfg.MemoryPath)
	before := outboxSnapshot(outboxDir)

	factory, registry, err := newAgentFactory(ctx, cfg, ego, logger)
	if err != nil {
		return err
	}
	failed := &failedToolCalls{}
	rt, err := factory.New(ctx, agentID, failed)
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

	response, err := rt.Session.Run(ctx, msg)
	if err != nil {
		return err
	}
	fmt.Println(response)
	echoOutboxToStdout(outboxDir, before)
	if len(failed.names) > 0 {
		return fmt.Errorf("one-shot turn: %d tool call(s) failed: %s",
			len(failed.names), strings.Join(failed.names, ", "))
	}
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
			"tools_dir", cfg.ToolsDir,
			"auth", "mtls")
		if err := srv.ListenAndServeTLS("", ""); !errors.Is(err, http.ErrServerClosed) {
			logger.Error("https server failed", "op", "serve", "error", err)
			return err
		}
	} else {
		logger.Info("agent listening",
			"op", "startup", "addr", cfg.Addr, "agent_id", agentID,
			"memory_path", cfg.MemoryPath, "workspace_addr", cfg.WorkspaceAddr,
			"tools_dir", cfg.ToolsDir,
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
