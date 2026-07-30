package main

import (
	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/message"
	"github.com/garamsh/gagent/internal/shell"
)

// newFactory builds the environment runtime factory both entry points
// use, so the resolved config reaches every environment the same way. A
// nil sink selects the default file outbox.
//
// After ADR 0012 the platform binary wires the four built-in tools:
// memory, files, message_send, and shell_exec. The shell_exec tool runs
// agent-supplied commands as sub-processes dropped to the agent UID per
// ADR 0006; the closure here constructs that executor from the resolved
// config.
func newFactory(
	cfg Config,
	newModel func(envID string) agent.Model,
	ego string,
	sink message.Sink,
) *gateway.Factory {
	return &gateway.Factory{
		Root:             cfg.DataRoot,
		NewModel:         newModel,
		Ego:              ego,
		Sink:             sink,
		ShellExecTimeout: cfg.ExecTimeout,
		NewShellExecutor: func() shell.Executor {
			return shell.NewProcessExecutor(shell.DefaultAgentUID, cfg.ExecTimeout)
		},
	}
}
