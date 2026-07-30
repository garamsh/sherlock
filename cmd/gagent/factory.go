package main

import (
	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/message"
)

// newFactory builds the environment runtime factory both entry points use,
// so the resolved config reaches every environment the same way. A nil
// sink selects the default file outbox.
//
// After ADR 0011 the platform binary no longer wires a sandbox runtime;
// the four built-in tools are memory, files, and message_send only.
func newFactory(
	cfg Config,
	newModel func(envID string) agent.Model,
	ego string,
	sink message.Sink,
) *gateway.Factory {
	return &gateway.Factory{
		Root:     cfg.DataRoot,
		NewModel: newModel,
		Ego:      ego,
		Sink:     sink,
	}
}
