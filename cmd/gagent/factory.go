package main

import (
	"log/slog"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/message"
	"github.com/garamsh/gagent/internal/tool"
)

// newFactory builds the agent runtime factory both agent modes use, so
// the resolved config reaches the agent the same way; a nil sink selects
// the sink consoleSinkFor resolves — the console sink when console.url is
// set, else the default file outbox. tools are the discovered remote
// tools joining the registry alongside message_send.
func newFactory(
	cfg Config,
	newModel func(agentID string) agent.Model,
	embedder memory.Embedder,
	summarizer memory.Summarizer,
	ego string,
	sink message.Sink,
	tools []tool.Tool,
) (*gateway.Factory, error) {
	newSink, err := consoleSinkFor(cfg, slog.Default())
	if err != nil {
		return nil, err
	}
	return &gateway.Factory{
		MemoryPath: cfg.MemoryPath,
		NewModel:   newModel,
		Embedder:   embedder,
		Summarizer: summarizer,
		Ego:        ego,
		Sink:       sink,
		NewSink:    newSink,
		Tools:      tools,
	}, nil
}
