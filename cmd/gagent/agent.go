package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/message"
	"github.com/garamsh/gagent/internal/sandbox"
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
			persona, err := resolvePersona(cmd)
			if err != nil {
				return err
			}
			envID, err := cmd.Flags().GetString("env")
			if err != nil {
				return err
			}
			msg, err := cmd.Flags().GetString("message")
			if err != nil {
				return err
			}
			return runAgentTurn(cmd.Context(), cfg, envID, msg, persona)
		},
	}
	cmd.Flags().String("env", "default", "environment ID to run the turn in")
	cmd.Flags().String("message", "", "message for the agent (required)")
	// Cannot fail: the flag is defined just above.
	_ = cmd.MarkFlagRequired("message")
	return cmd
}

// runAgentTurn builds the environment runtime (same wiring as the gateway,
// with a stdout sink in place of the file outbox) and runs one turn.
func runAgentTurn(ctx context.Context, cfg Config, envID, msg, persona string) error {
	// Logs go to stderr so stdout carries only the agent's output.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	docker, err := sandbox.NewDockerRuntime()
	if err != nil {
		logger.Error("docker runtime unavailable", "op", "startup", "error", err)
		return err
	}
	newModel, err := newModelFunc(cfg.Model)
	if err != nil {
		return err
	}
	mcpTools, closeMCP, err := startMCPServers(ctx, cfg.MCP.Servers, logger)
	if err != nil {
		return err
	}
	defer func() {
		if err := closeMCP(); err != nil {
			logger.Error("mcp shutdown failed", "op", "shutdown", "error", err)
		}
	}()
	factory := &gateway.Factory{
		Root:        cfg.DataRoot,
		NewModel:    newModel,
		Persona:     persona,
		Sandbox:     docker,
		Image:       cfg.Image,
		ExecTimeout: cfg.ExecTimeout,
		Sink:        stdoutSink{},
		Tools:       mcpTools,
	}
	rt, err := factory.New(ctx, envID, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := rt.Close(context.Background()); err != nil {
			logger.Error("environment close failed", "op", "shutdown", "error", err)
		}
	}()

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
