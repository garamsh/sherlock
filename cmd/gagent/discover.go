package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/garamsh/gagent/internal/tool"
)

// Discovery bounds (ADR 0018): the agent lists each configured tool
// server at startup with bounded retries — a workspace may still be
// starting when the agent comes up.
const (
	discoverAttempts = 5
	discoverDelay    = time.Second
)

// discoverTools lists every configured tool server and returns one remote
// tool per discovered descriptor. A server that stays unreachable through
// the bounded retries fails startup with an error naming it; a server
// that disappears mid-run surfaces later as ordinary tool execution
// errors.
func discoverTools(ctx context.Context, servers []string, logger *slog.Logger) ([]tool.Tool, error) {
	var tools []tool.Tool
	for _, server := range servers {
		descs, err := discoverServer(ctx, server, logger)
		if err != nil {
			return nil, err
		}
		for _, d := range descs {
			tools = append(tools, tool.NewRemoteTool(d, server))
		}
	}
	return tools, nil
}

// discoverServer lists one server's descriptors, retrying on failure
// until the attempt budget runs out.
func discoverServer(ctx context.Context, server string, logger *slog.Logger) ([]tool.Descriptor, error) {
	var err error
	for attempt := 1; attempt <= discoverAttempts; attempt++ {
		var descs []tool.Descriptor
		descs, err = tool.Discover(ctx, server)
		if err == nil {
			logger.Info("tool server discovered",
				"op", "discover", "server", server, "tools", len(descs))
			return descs, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		logger.Warn("tool server unreachable; retrying",
			"op", "discover", "server", server, "attempt", attempt, "error", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(discoverDelay):
		}
	}
	return nil, fmt.Errorf("tool server %s unreachable after %d attempts: %w",
		server, discoverAttempts, err)
}
