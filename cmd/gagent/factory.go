package main

import (
	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/message"
	"github.com/garamsh/gagent/internal/sandbox"
	"github.com/garamsh/gagent/internal/tool"
)

// newFactory builds the environment runtime factory both entry points use,
// so the resolved config — image, quotas, exec timeout — reaches every
// environment the same way. A nil sink selects the default file outbox.
func newFactory(
	cfg Config,
	docker sandbox.Runtime,
	newModel func(envID string) agent.Model,
	ego string,
	tools []tool.Tool,
	sink message.Sink,
) *gateway.Factory {
	return &gateway.Factory{
		Root:        cfg.DataRoot,
		NewModel:    newModel,
		Ego:         ego,
		Sandbox:     docker,
		Image:       cfg.Image,
		Policy:      cfg.Policy,
		ExecTimeout: cfg.ExecTimeout,
		Sink:        sink,
		Tools:       tools,
	}
}
