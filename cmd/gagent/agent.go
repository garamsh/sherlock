package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/garamsh/gagent/internal/message"
)

// newAgentCommand returns the "gagent agent" subcommand: it runs a single
// turn of one environment's agent with the configured model provider,
// printing outbound messages and the final response to stdout.
func newAgentCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Run one agent turn and print the response",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := resolveConfig(cmd)
			if err != nil {
				return err
			}
			ego, err := resolveEgo(cmd)
			if err != nil {
				return err
			}
			agentID, err := cmd.Flags().GetString("env")
			if err != nil {
				return err
			}
			msg, err := cmd.Flags().GetString("message")
			if err != nil {
				return err
			}
			return runAgentTurn(cmd.Context(), cfg, agentID, msg, ego)
		},
	}
	cmd.Flags().String("env", "default", "environment ID to run the turn in")
	cmd.Flags().String("message", "", "message for the agent (required)")
	// Cannot fail: the flag is defined just above.
	_ = cmd.MarkFlagRequired("message")
	return cmd
}

// runAgentTurn builds the environment runtime (same wiring as the gateway,
// with a stdout sink in place of the file outbox), captures the message as
// a user-turn memory (ADR 0016), and runs one turn.
//
// The factory wires the four built-in tools (memory, files, message_send,
// shell_exec) per ADR 0012; the shell_exec executor runs agent-supplied
// commands as sub-processes dropped to the agent UID.
func runAgentTurn(ctx context.Context, cfg Config, agentID, msg, ego string) error {
	// Logs go to stderr so stdout carries only the agent's output.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	newModel, err := newModelFunc(cfg.Model)
	if err != nil {
		return err
	}
	embedder, err := newEmbedderFunc(cfg.Embedder)
	if err != nil {
		return err
	}
	factory, err := newFactory(cfg, newModel, embedder, ego, stdoutSink{})
	if err != nil {
		return err
	}
	rt, err := factory.New(ctx, agentID, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := rt.Close(context.Background()); err != nil {
			logger.Error("environment close failed", "op", "shutdown", "error", err)
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

// stdoutSink is the agent subcommand's message.Sink: it prints each
// outbound message to stdout instead of writing the file outbox.
type stdoutSink struct{}

// Send implements message.Sink.
func (stdoutSink) Send(_ context.Context, msg message.Message) error {
	fmt.Printf("[%s -> %s] %s\n", msg.Channel, msg.Target, msg.Content)
	return nil
}
