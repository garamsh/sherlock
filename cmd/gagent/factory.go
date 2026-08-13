package main

import (
	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/tool"
)

// newFactory builds the agent runtime factory both agent modes use, so
// the resolved config reaches the agent the same way. The tool client
// is the platform protocol client; the agent no longer compiles any
// tool-shaped code (ADR 0018 §Decision).
func newFactory(
	cfg Config,
	newModel func(agentID string) agent.Model,
	embedder memory.Embedder,
	summarizer memory.Summarizer,
	ego string,
	tools *tool.Client,
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
