package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/tool"
)

// stubTool is a name-only tool for collision tests.
type stubTool string

func (t stubTool) Name() string                  { return string(t) }
func (t stubTool) Description() string           { return "" }
func (t stubTool) InputSchema() json.RawMessage  { return nil }
func (t stubTool) OutputSchema() json.RawMessage { return nil }
func (t stubTool) Call(context.Context, json.RawMessage) (json.RawMessage, error) {
	return nil, errors.New("stub")
}

func TestCheckToolCollisions(t *testing.T) {
	tests := []struct {
		name    string
		tools   []tool.Tool
		wantErr error
	}{
		{name: "no collision", tools: []tool.Tool{stubTool("time_now"), stubTool("weather")}},
		{name: "built-in collision", tools: []tool.Tool{stubTool("memory")}, wantErr: tool.ErrDuplicateTool},
		{name: "mcp-vs-mcp collision", tools: []tool.Tool{stubTool("dup"), stubTool("dup")}, wantErr: tool.ErrDuplicateTool},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkToolCollisions(tt.tools)
			if tt.wantErr == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
}

func TestStartMCPServers_ToleratesUnstartableServer(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	tools, closeAll, err := startMCPServers(context.Background(), []MCPServerConfig{
		{Name: "broken", Command: "gagent-test-nonexistent-command"},
	}, logger)
	require.NoError(t, err)
	assert.Empty(t, tools)
	assert.NoError(t, closeAll())
}

func TestStartMCPServers_InvalidConfigIsSkipped(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	tools, closeAll, err := startMCPServers(context.Background(), []MCPServerConfig{
		{Name: "no-command"},
	}, logger)
	require.NoError(t, err)
	assert.Empty(t, tools)
	assert.NoError(t, closeAll())
}
