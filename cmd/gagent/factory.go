package main

import (
	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/config"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/tool"
)

// newFactory builds the agent runtime factory both agent modes use, so
// the resolved config reaches the agent the same way. The tool
// registry is the script-tool runtime the agent loads at startup; the
// agent no longer compiles any tool-shaped code (ADR 0017 §Decision,
// ADR 0009 §Decision).
func newFactory(
	cfg config.Config,
	newModel func(agentID string) agent.Model,
	embedder memory.Embedder,
	summarizer memory.Summarizer,
	ego string,
	tools *tool.Registry,
) (*gateway.Factory, error) {
	return &gateway.Factory{
		MemoryPath: cfg.MemoryPath,
		NewModel:   newModel,
		Embedder:   embedder,
		Summarizer: summarizer,
		Ego:        ego,
		Tools:      tools,
	}, nil
}
