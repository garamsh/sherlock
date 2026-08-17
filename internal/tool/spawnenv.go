package tool

import (
	"fmt"
	"os"
)

// Spawn environment variable names. Which variables a tool reads is
// part of the call contract this package owns (ADR 0009 §Decision: the
// runner inherits the agent's env), so the names live beside the runner
// that hands them on rather than at whichever entry point resolves
// their values.
const (
	// WorkspaceAddrEnv names the workspace's gRPC endpoint, which the
	// files and shell_exec tools dial.
	WorkspaceAddrEnv = "GAGENT_WORKSPACE_ADDR"

	// OutboxDirEnv names the directory the message_send tool delivers
	// into.
	OutboxDirEnv = "GAGENT_OUTBOX_DIR"

	// AgentIDEnv names the ID of the agent this process serves, which
	// message_send stamps on every message it delivers so a reader of
	// the outbox knows who sent it (ADR 0016).
	//
	// It carries the ID the agent resolved, not one an operator sets:
	// --agent-id is a flag and this value is published after it is
	// read, so a value already in the environment is replaced.
	AgentIDEnv = "GAGENT_AGENT_ID"
)

// SetSpawnEnv publishes the values the shipped tools read from their
// own process environment. The runner spawns each tool with the
// platform's environment (runner.go), so setting them here is what puts
// them in front of every tool the registry loads.
//
// A tool's credentials are not among them: a secret reaches its tool
// through the environment the operator already gave the platform, never
// through a value the platform materializes (ADR 0003, ADR 0013). The
// github tool's GH_TOKEN is the standing example — the platform never
// sets it, and a tool that needs one and does not find one fails on its
// own terms.
func SetSpawnEnv(workspaceAddr, outboxDir, agentID string) error {
	for _, v := range []struct{ name, value string }{
		{WorkspaceAddrEnv, workspaceAddr},
		{OutboxDirEnv, outboxDir},
		{AgentIDEnv, agentID},
	} {
		if err := os.Setenv(v.name, v.value); err != nil {
			return fmt.Errorf("set %s: %w", v.name, err)
		}
	}
	return nil
}
