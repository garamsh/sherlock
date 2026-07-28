package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/tool"
	"github.com/garamsh/gagent/internal/tool/mcp"
)

// startMCPServers starts every configured MCP server and returns the
// discovered tools plus a hook closing all started servers. A server that
// fails to start is logged and skipped; a tool name colliding with a
// built-in or another MCP tool fails startup with tool.ErrDuplicateTool.
func startMCPServers(ctx context.Context, servers []MCPServerConfig, logger *slog.Logger) ([]tool.Tool, func() error, error) {
	var (
		tools   []tool.Tool
		started []*mcp.Server
	)
	closeAll := func() error {
		var err error
		for _, srv := range started {
			err = errors.Join(err, srv.Close())
		}
		return err
	}
	for _, cfg := range servers {
		srv, err := mcp.Start(ctx, mcp.Config{
			Name:    cfg.Name,
			Command: cfg.Command,
			Args:    cfg.Args,
			Env:     cfg.Env,
			Timeout: cfg.Timeout,
		})
		if err != nil {
			logger.Error("mcp server failed to start", "op", "mcp_start", "server", cfg.Name, "error", err)
			continue
		}
		started = append(started, srv)
		tools = append(tools, srv.Tools()...)
		logger.Info("mcp server started", "op", "mcp_start", "server", cfg.Name, "tools", len(srv.Tools()))
	}
	if err := checkToolCollisions(tools); err != nil {
		return nil, nil, errors.Join(err, closeAll())
	}
	return tools, closeAll, nil
}

// checkToolCollisions reserves the built-in tool names and registers the
// discovered MCP tools into a scratch registry, so a name collision
// surfaces as tool.ErrDuplicateTool at startup instead of at first
// environment use.
func checkToolCollisions(mcpTools []tool.Tool) error {
	registry := tool.NewRegistry()
	for _, name := range gateway.BuiltinToolNames() {
		if err := registry.Register(namedTool(name)); err != nil {
			return err
		}
	}
	for _, t := range mcpTools {
		if err := registry.Register(t); err != nil {
			return err
		}
	}
	return nil
}

// namedTool is a name-only tool.Tool reserving a built-in name in the
// startup collision check.
type namedTool string

// Name returns the reserved name.
func (t namedTool) Name() string { return string(t) }

// Description returns no description; the tool is never advertised.
func (t namedTool) Description() string { return "" }

// InputSchema returns no schema; the tool is never invoked.
func (t namedTool) InputSchema() json.RawMessage { return nil }

// OutputSchema returns no schema; the tool is never invoked.
func (t namedTool) OutputSchema() json.RawMessage { return nil }

// Call is never invoked; namedTool only reserves a name.
func (t namedTool) Call(context.Context, json.RawMessage) (json.RawMessage, error) {
	return nil, errors.New("namedTool is a placeholder and cannot be called")
}
