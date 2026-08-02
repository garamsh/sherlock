package main

import (
	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/message"
	"github.com/garamsh/gagent/internal/shell"
)

// newFactory builds the environment runtime factory both entry points
// use, so the resolved config reaches every environment the same way; a
// nil sink selects the default file outbox. The config's tools: section
// is validated here — the one validation point both entry points share —
// so an invalid script tool fails platform startup instead of the first
// message (ADR 0013).
func newFactory(
	cfg Config,
	newModel func(envID string) agent.Model,
	embedder memory.Embedder,
	ego string,
	sink message.Sink,
) (*gateway.Factory, error) {
	factory := &gateway.Factory{
		Root:             cfg.DataRoot,
		NewModel:         newModel,
		Embedder:         embedder,
		Ego:              ego,
		Sink:             sink,
		ShellExecTimeout: cfg.ExecTimeout,
		NewShellExecutor: func() shell.Executor {
			return shell.NewProcessExecutor(shell.DefaultAgentUID, cfg.ExecTimeout)
		},
		Tools: cfg.Tools,
	}
	if err := factory.ValidateTools(); err != nil {
		return nil, err
	}
	return factory, nil
}
